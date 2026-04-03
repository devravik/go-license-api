# Go License API

A self-hosted license validation service built in Go. You run it alongside your product, call it to check if a license key is valid, and it handles everything else — multi-tenancy, seat tracking, signed offline licenses, key rotation, webhooks, and audit logging.

---

## What it does

You have a product. You sell licenses. Your app needs to check if a given license key is valid before granting access. That's what this service handles.

Your app sends a license key and product ID. The service returns whether it's valid, what plan it's on, which features are included, and how many seats are left.

Beyond the basics, it covers the operational complexity that comes with real-world licensing:

- **Multi-tenant** — one instance serves multiple independent products/teams, each isolated with their own licenses, rate limits, and API keys
- **Seat and activation tracking** — seat-based, concurrent float, or consumption-based models
- **Offline / signed licenses** — Ed25519-signed payloads clients verify locally without a network call
- **Trial and grace periods** — time-limited trials and configurable grace windows after expiry
- **Key rotation** — zero-downtime rotation for tenant API keys and signing keys
- **Webhooks** — signed HTTP callbacks on license events (expiry, activation, revocation)
- **Audit log** — immutable record of every action with actor, IP, timestamp, and outcome
- **IP allowlisting** — per-tenant and admin CIDR restrictions
- **Rate limiting** — per-tenant token bucket with adaptive abuse blocking
- **Admin CLI** — full control-plane management from the terminal

---

## How it works

Two planes, one database, no shared code paths.

```
                         DATA PLANE  /licenses/*
 ┌─────────┐
 │ Request │
 └────┬────┘
      │
      ▼
 ┌──────────────┐    reject     ┌─────────────────┐
 │  Tenant Auth │ ─────────────►│  403 / 401      │
 └──────┬───────┘               └─────────────────┘
        │
        ▼
 ┌──────────────┐    blocked    ┌─────────────────┐
 │   IP Guard   │ ─────────────►│  403 Forbidden  │
 └──────┬───────┘               └─────────────────┘
        │
        ▼
 ┌──────────────┐   throttled   ┌─────────────────┐
 │ Rate Limiter │ ─────────────►│  429 Too Many   │
 └──────┬───────┘               └─────────────────┘
        │
        ▼
 ┌──────────────┐
 │  Job Queue   │  (buffered channel)
 └──────┬───────┘
        │
        ▼
 ┌──────────────┐
 │ Worker Pool  │  (fixed goroutines)
 └──────┬───────┘
        │
        ▼
 ┌──────────────────────────────────────┐
 │             Validation               │
 │                                      │
 │   L1 hit ──► return immediately      │
 │      │                               │
 │   L1 miss                            │
 │      │                               │
 │   L2 hit (Redis) ──► backfill L1     │
 │      │                               │
 │   L2 miss                            │
 │      │                               │
 │   L3 (PostgreSQL) ──► backfill L1+L2 │
 │      │                               │
 │   full miss ──► invalid              │
 └──────┬───────────────────────────────┘
        │
        ├──────────────────────────────► Webhook Dispatch (async)
        │
        ▼
 ┌──────────────┐
 │  Audit Log   │
 └──────┬───────┘
        │
        ▼
 ┌─────────────────┐
 │    Response     │
 └─────────────────┘


                        CONTROL PLANE  /admin/*

 ┌─────────────────┐    reject    ┌──────────────────┐
 │ X-Admin-Key     │ ────────────►│  401 Unauthorized│
 │ + IP CIDR check │              └──────────────────┘
 └────────┬────────┘
          │
          ▼
 ┌─────────────────┐
 │  Admin Handler  │  (no queue, no rate limit)
 └────────┬────────┘
          │
          ▼
 ┌─────────────────┐     ┌──────────────────────────┐
 │   PostgreSQL    │────►│  Cache update/invalidate  │
 └─────────────────┘     └──────────────────────────┘
```

PostgreSQL is the source of truth for persistence. The in-memory LRU cache is the source of truth at runtime — validation never queries the database on a cache hit. Any admin write goes to Postgres first, then updates or invalidates the cache immediately.

Deep dive: [docs/architecture.md](docs/architecture.md)

---

## Quick start

**Requirements:** Go 1.21+, PostgreSQL 14+

```bash
git clone https://github.com/devravik/go-license-api.git
cd go-license-api

cp env.example .env        # fill in DB credentials and ADMIN_API_KEY

docker compose up -d       # start Postgres (or bring your own)
go run ./cmd/migrate       # run migrations
go run main.go             # start the server
```

Server starts on `http://localhost:3000`.

**Minimum `.env`:**

```env
APP_MODE=multi
PORT=3000
ADMIN_API_KEY=your_secret_key
DB_HOST=127.0.0.1
DB_PORT=5432
DB_DATABASE=golicense
DB_USERNAME=postgres
DB_PASSWORD=postgres
```

Redis is optional. Skip `REDIS_URL` and it runs fine with the local LRU cache only.

Full configuration reference: [env.example](env.example)

---

## Basic usage

**1. Create a tenant**

```bash
go run ./cmd/cli tenant create --rps=100 --burst=200 --pretty
```

Returns a tenant ID and API key. Use both in all subsequent requests for that tenant.

**2. Create a license**

```bash
go run ./cmd/cli license create \
  --tenant=tenant_1 \
  --key=ABC-123 \
  --product=v-plugin \
  --expires=2026-12-31 \
  --meta='{"plan":"pro"}'
```

**3. Validate a license**

```bash
curl -X POST http://localhost:3000/licenses/validate \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: tenant_1" \
  -H "X-API-Key: your_api_key" \
  -d '{"key": "ABC-123", "product": "v-plugin"}'
```

```json
{
  "valid": true,
  "meta": {
    "plan": "pro",
    "expires_at": "2026-12-31",
    "seats_total": 10,
    "seats_used": 2,
    "trial": false,
    "features": ["sso", "audit-log"]
  }
}
```

**4. Activate a seat**

```bash
curl -X POST http://localhost:3000/licenses/activate \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: tenant_1" \
  -H "X-API-Key: your_api_key" \
  -d '{"key": "ABC-123", "product": "v-plugin", "machine_id": "hw-fingerprint-hash", "hostname": "workstation-1"}'
```

**5. Get a signed offline license**

```bash
curl http://localhost:3000/licenses/ABC-123/signed \
  -H "X-Tenant-ID: tenant_1" \
  -H "X-API-Key: your_api_key"
```

Returns a signed JSON payload. The client verifies it locally using the public key from `GET /.well-known/jwks.json` — no network call needed at verification time.

---

## Deployment

**Docker:**

```bash
docker build -f deployments/docker/Dockerfile -t go-license-api .
docker compose -f deployments/docker/docker-compose.yml up
```

**Kubernetes:**

```bash
kubectl apply -f deployments/k8s/
# or via Helm
helm install go-license-api deployments/k8s/helm/
```

**Health checks:** `GET /healthz` (liveness), `GET /readyz` (readiness)

---

## Docs

| Topic | File |
|---|---|
| Full API reference | [docs/api.md](docs/api.md) |
| CLI reference | [docs/cli.md](docs/cli.md) |
| Architecture deep dive | [docs/architecture.md](docs/architecture.md) |
| Setup guide | [docs/setup.md](docs/setup.md) |
| OpenAPI spec | [docs/openapi.yaml](docs/openapi.yaml) |

---

## Project structure

```
cmd/
  server/         entrypoint
  cli/            admin CLI
  migrate/        database migrations

internal/
  domain/         business models and rules (no DB, no HTTP)
  app/            use cases, orchestration
  infrastructure/ DB, cache (LRU + Redis), crypto, rate limiter
  http/           handlers, middleware, DTOs, router
  worker/         fixed worker pool
  audit/          audit log writer
  webhook/        dispatcher and retry scheduler
  server/         server setup, graceful shutdown

migrations/       SQL migration files
deployments/      Docker and Kubernetes manifests
docs/             guides and OpenAPI spec
```

---

## Maintainer

**Ravi K Gupta**

- **Website**: [devravik.github.io](https://devravik.github.io/)
- **Email**: `dev.ravikgupt@gmail.com`
- **LinkedIn**: [linkedin.com/in/ravi-k-dev](https://www.linkedin.com/in/ravi-k-dev)
- **GitHub**: [github.com/devravik](https://github.com/devravik)

---

## License

MIT. See [LICENSE](LICENSE).
