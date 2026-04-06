# Go License API

High-performance, self-hosted license validation service built in Go.

Run it alongside your product. Your app calls it to validate licenses. Everything else-multi-tenancy, seat tracking, offline licenses, key rotation, webhooks, and audit logging-is handled for you.

![Go Version](https://img.shields.io/badge/go-1.21-blue) ![License](https://img.shields.io/badge/license-MIT-green) ![Status](https://img.shields.io/badge/status-production--ready-brightgreen)

---

## Who is this for

- SaaS products needing reliable license enforcement
- Desktop apps requiring offline activation/verification
- Plugin/theme ecosystems (WordPress, IDEs, design tools)
- API-first products with usage-based/burstable billing
- Teams scaling beyond simple DB-based validation in the main app

---

## Use Cases

- Validate licenses for desktop apps (offline + online modes)
- Enforce subscription plans and feature tiers in SaaS
- Limit seats or concurrent devices per license
- Protect plugins/themes from piracy with signed offline licenses
- Enable feature flags based on license tier or entitlements

---

## What’s new

- Not-before (`nbf`) support for licenses (scheduled validity)
- Explicit revocation IDs for immediate invalidation and auditability
- Stronger write-through cache rules on admin updates
- OpenAPI spec and CI workflow for validation/publishing
- Load-test docs and CLI improvements

See: `docs/offline_signed_licenses.md`, `docs/loadtest.md`, `docs/api.md`

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

**Requirements:** Go 1.21+, PostgreSQL 14+, Docker (optional)

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

Most apps start with “check in DB if license exists.” That becomes a bottleneck and a maintenance headache:

- slow under load and hard to scale across regions
- no clean support for offline validation
- tightly coupled with application logic and releases
- operational risk: DB outages take licensing down

This service isolates license validation into a **dedicated system**:

* your app makes a single HTTP call
* validation runs entirely from memory on cache hits
* no database queries in the hot path

Result: fast, predictable, and maintainable licensing infrastructure.

---

## Mental model (high-level)

```
Your App ──→ License API ──→ L1 Cache (in-memory) ──→ Response
                          └─→ L2 Cache (Redis, optional)
                          └─→ PostgreSQL (control plane only)
```

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

## Cache model and consistency (critical)

- Working-set only, never preload entire dataset
- L1 = in-process bounded LRU; optional L2 = Redis
- Validation behavior:
  - L1 hit → return
  - L2 hit → backfill L1 → return
  - Full miss → return invalid (no DB calls)
- Control plane writes:
  - DB write → cache update (write-through) or invalidate-then-refresh
  - Always overwrite/invalidate whole objects (no partial mutation)

> Note: Cache miss returns invalid by design to keep validation path database-free.
> For production, enable Redis (L2) and/or warm critical entries to avoid false negatives after cold starts or evictions.

See details in `development/03_infrastructure_cache.md`.

---

## What it handles

* Multi-tenant isolation (per-tenant API keys, limits)
* Seat-based, floating, and usage-based licensing
* Signed offline licenses (Ed25519)
* Trial and grace periods
* Not-before and explicit revocation IDs
* Optional client/device binding via activations (prevents license sharing)
* Key rotation (zero downtime)
* Webhooks (signed callbacks)
* Audit logging (immutable, traceable)
* IP allowlisting
* Per-tenant rate limiting (token bucket)

---

## Sizing and cost guidance

Assumptions:
- Reserve ~40% RAM for Go runtime, queues, workers, and overhead
- Allocate ~60% to L1 cache working set
- Average L1 license entry ~1.5 KB total (struct + maps + strings)
- P95 latency assumes cache hits; misses return invalid without DB calls

| RAM (instance) | L1 cache budget (~60%) | Approx. cached licenses | Suggested workers | Suggested queue | Est. p95 @ 1k RPS | Notes |
| -------------- | ---------------------- | ----------------------- | ----------------- | --------------- | ----------------- | ----- |
| 256 MB         | ~150 MB                | ~100k                   | 8                 | 5k              | 0.5–0.8 ms        | Good for small SaaS, single product |
| 512 MB         | ~300 MB                | ~200k                   | 8–10              | 10k             | 0.5–0.8 ms        | Most teams start here |
| 1 GB           | ~600 MB                | ~420k                   | 12                | 15k             | 0.5–0.9 ms        | Room for webhooks/audit spikes |
| 2 GB           | ~1.2 GB               | ~840k                   | 16                | 20k             | 0.5–1.0 ms        | Heavier multi-tenant workloads |
| 4 GB           | ~2.4 GB               | ~1.6M                   | 24                | 30k             | 0.5–1.0 ms        | High churn / broad product lines |

Notes:
- Use Redis as L2 to increase effective working set without increasing instance RAM.
- Throughput scales mostly with CPU and worker count; latency depends on cache hit ratio.
- Rate limiting happens before queue; tune limiter to protect workers under bursts.

For load-test methodology and raw numbers, see `docs/loadtest.md`.

---

## Client-side recommendations (resilience)

- Cache the last known-valid response client-side
- Allow short grace periods on transient network failure
- Retry with exponential backoff and jitter
- Prefer short timeouts on the validation call and fail closed after grace
- For offline apps, use signed licenses and verify with Ed25519 locally

---

## Why not Stripe / Firebase / just your DB?

| Approach      | Problem |
|--------------|---------|
| DB in app    | Slow under load, tightly coupled, no offline, hard multi-tenant isolation |
| Stripe only  | Billing SDKs, but no offline license validation, no seat/device enforcement |
| Firebase     | General auth/data; not license-focused; offline signing not first-class |
| This project | Fast, cache-first, offline-signed, multi-tenant, bounded concurrency |

---

## Configuration

Core:
- `ADMIN_API_KEY` (required): control plane authentication
- `PORT` or `APP_PORT` (default: 3000)
- `APP_ENV` (default: development)

Database:
- `DATABASE_URL` or `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD`

Workers and queues:
- `WORKER_COUNT` (default: 8)
- `WORKER_QUEUE_SIZE` (default: 5000)
- `VALIDATION_TIMEOUT`, `WORKER_TIMEOUT`, `CLIENT_TIMEOUT`

Rate limiting:
- `LIMITER_ENABLED` (default: true)
- `LIMITER_FAILS_PER_MINUTE`, `LIMITER_GLOBAL_FAILS_PER_MINUTE`
- Optional Redis for limiter state: `LIMITER_REDIS_ENABLED`, `LIMITER_REDIS_URL`

Admin security:
- `ADMIN_ALLOWED_CIDRS` (comma-separated)

Signing and webhooks:
- `SIGNING_KEY_PATH` (for offline signed licenses)
- `WEBHOOK_ENCRYPTION_KEY`

See `env.example` for a complete reference.

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

## How to use the response in your app

Example (pseudocode):

```python
resp = call_license_api()
if resp.get("valid") is True:
    features = resp["meta"].get("features", [])
    enable_features(features)
    set_plan(resp["meta"]["plan"]["name"])
else:
    # optional: apply grace if recent valid cached locally
    if has_recent_valid_cache():
        allow_temporary_access()
    else:
        block_access_and_prompt_sign_in_or_purchase()
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
| Offline Signed Licenses| docs/offline_signed_licenses.md |
| Load Testing           | docs/loadtest.md     |

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
