# Go License API

High-performance, self-hosted license validation service built in Go.

Run it alongside your product. Your app calls it to validate licenses. Everything else-multi-tenancy, seat tracking, offline licenses, key rotation, webhooks, and audit logging-is handled for you.

---

## Highlights

* Sub-millisecond validation
* Multi-tenant with strict isolation
* Built-in rate limiting and abuse protection
* Offline license verification (Ed25519, no network required)
* Production-ready: audit logs, webhooks, key rotation
* Predictable performance under load (worker pool + bounded queue)

---

## Quick Start

**Requirements:** Go 1.21+, PostgreSQL 14+

```bash
git clone https://github.com/devravik/go-license-api.git
cd go-license-api

cp env.example .env           # set ADMIN_API_KEY + DB credentials

docker compose up -d          # start Postgres
go run ./cmd/migrate          # run migrations
go run ./cmd/server           # start server on :3000
```

Create a tenant and license:

```bash
go run ./cmd/cli tenant create --pretty
go run ./cmd/cli license create \
  --tenant=<tenant_id> \
  --key=ABC-123 \
  --product=my-app \
  --expires=2027-01-01
```

Validate:

```bash
curl -X POST http://localhost:3000/licenses/validate \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: <tenant_id>" \
  -H "X-API-Key: <api_key>" \
  -d '{"key": "ABC-123", "product_id": "prod_xxxxxxxx"}'
```

---

## Why this exists

Most applications implement license validation as a database lookup inside the main app. That approach breaks down at scale:

* validation logic spreads across the codebase
* latency spikes affect unrelated features
* multi-tenancy and offline support become complex

This service isolates license validation into a **dedicated system**:

* your app makes a single HTTP call
* validation runs entirely from memory on cache hits
* no database queries in the hot path

Result: fast, predictable, and maintainable licensing infrastructure.

---

## Architecture (Simplified)

```
Request
  ↓
Tenant Auth → IP Guard → Rate Limit
  ↓
Queue → Worker Pool
  ↓
Validation (L1 → L2 cache-only)
  ↓
Audit Log + Webhooks
  ↓
Response
```

### Key design principles

* **No DB in validation path** (served from in-memory cache)
* **Control plane isolated** from runtime API
* **Bounded concurrency** via worker pool
* **Write-through cache consistency** on admin updates

---

## Two Planes

**Data Plane (`/licenses/*`)**

* validation, activation, usage
* fully cached, high-throughput
* rate-limited and protected

**Control Plane (`/admin/*`)**

* tenants, licenses, configuration
* writes to DB, updates cache
* no queue, no rate limiting
* secured via `ADMIN_API_KEY` + CIDR

---

## What it handles

* Multi-tenant isolation (per-tenant API keys, limits)
* Seat-based, floating, and usage-based licensing
* Signed offline licenses (Ed25519)
* Trial and grace periods
* Key rotation (zero downtime)
* Webhooks (signed callbacks)
* Audit logging (immutable, traceable)
* IP allowlisting
* Per-tenant rate limiting (token bucket)

---

## Example Response

```json
{
  "valid": true,
  "meta": {
    "license_id": "lic_xxxxxxxx",
    "status": "active",
    "type": "plan",
    "plan_id": "plan_xxxxxxxx",
    "plan": {
      "id": "plan_xxxxxxxx",
      "name": "Pro"
    },
    "product_id": "prod_xxxxxxxx",
    "product": {
      "id": "prod_xxxxxxxx",
      "name": "My App"
    },
    "expires_at": "2026-12-31T00:00:00Z",
    "seats_total": 10,
    "unlimited_seats": false,
    "trial": false,
    "features": ["sso", "audit-log"]
  }
}
```

---

## Deployment

**Docker**

```bash
docker build -f deployments/docker/Dockerfile -t go-license-api .
docker compose -f deployments/docker/docker-compose.yml up
```

**Kubernetes**

```bash
kubectl apply -f deployments/k8s/
# or Helm
helm install go-license-api deployments/k8s/helm/
```

**Health endpoints**

* `GET /healthz` - liveness
* `GET /readyz` - readiness

---

## Docs

| Topic                  | Link                 |
| ---------------------- | -------------------- |
| API Reference          | docs/api.md          |
| CLI Reference          | docs/cli.md          |
| Architecture Deep Dive | docs/architecture.md |
| Setup Guide            | docs/setup.md        |
| OpenAPI Spec           | docs/openapi.yaml    |

---

## Project Structure

```
cmd/
  server/         entrypoint
  cli/            admin CLI
  migrate/        database migrations

internal/
  domain/         business rules
  app/            use cases
  infrastructure/ DB, cache, crypto, rate limiter
  http/           handlers, middleware
  worker/         worker pool
  audit/          audit logging
  webhook/        dispatcher
  server/         bootstrap

migrations/
deployments/
docs/
```

---

## Maintainer

**Ravi K Gupta**

* Website: https://devravik.github.io/
* Email: [dev.ravikgupt@gmail.com](mailto:dev.ravikgupt@gmail.com)
* LinkedIn: https://www.linkedin.com/in/ravi-k-dev
* GitHub: https://github.com/devravik

---

## License

MIT - see LICENSE
