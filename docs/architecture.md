# Architecture

This document covers how the service is structured, how the two planes relate, how the cache works, and the key design decisions behind it.

---

## Two planes, one database

The service is split into a data plane and a control plane. They share the same PostgreSQL database but never share code paths, middleware, or infrastructure components.

**Data plane** — everything under `/licenses/*`. This is the hot path: license validation, seat activation, consumption reporting. It is optimized for throughput and latency. The database is never queried on a cache hit. Requests go through tenant auth, IP guard, rate limiting, a buffered job queue, and a fixed worker pool before reaching validation logic.

**Control plane** — everything under `/admin/*`. This handles management: creating tenants, issuing licenses, managing products, rotating keys. It writes to PostgreSQL and updates the cache immediately. It bypasses the worker pool and rate limiter entirely. Requires `X-Admin-Key`.

The separation is intentional. Management operations cannot starve or block validation requests. A slow admin write does not affect validation latency.

---

## Request lifecycle (data plane)

```
HTTP Request
     │
     ▼
Tenant Auth middleware
  - Resolves tenant from X-Tenant-ID + X-API-Key headers
  - Validates against L1 cache (no DB call)
  - Rejects with 401/403 if invalid or suspended
     │
     ▼
IP Guard middleware
  - Checks request IP against tenant allowlist (CIDR)
  - Rejects with 403 if not in allowlist
     │
     ▼
Rate Limiter middleware
  - Per-tenant token bucket (in-memory)
  - Adaptive fail limiter blocks IPs/tenants with repeated failures
  - Rejects with 429 if throttled
     │
     ▼
Job Queue (buffered channel)
  - Request is wrapped as a job and pushed to the channel
  - Backpressure: if queue is full, request is rejected with 503
     │
     ▼
Worker Pool (fixed goroutines)
  - Fixed number of goroutines (WORKER_COUNT) pulling from the channel
  - Each job runs with a context timeout (WORKER_TIMEOUT)
  - Context cancellation stops slow DB operations from blocking the pool
     │
     ▼
Validation logic
  - L1 hit  → return immediately (sub-millisecond)
  - L1 miss → check L2 (Redis, if configured) → backfill L1
  - L2 miss → query L3 (PostgreSQL) → backfill L1 + L2
  - Full miss → return { valid: false }
     │
     ├──► Webhook dispatch (async, non-blocking)
     │
     ▼
Audit log (async writer)
     │
     ▼
Response
```

---

## Cache design

The cache is the most important piece. Validation must never wait on a database or network call in the hot path.

### Three layers

```
L1: In-memory LRU (always on)
  - Bounded by CACHE_L1_MAX_ENTRIES (default 10,000)
  - Per-process; not shared across instances
  - Evicts least-recently-used entries when at capacity
  - ~80MB at 100,000 entries for typical license records

L2: Redis (optional)
  - Enabled when REDIS_URL is set
  - Shared across all instances in a multi-node deployment
  - Reduces database pressure; acts as a warm shared pool
  - When absent, the service falls back directly to L3

L3: PostgreSQL (source of truth)
  - Queried only on a full cache miss (L1 and L2 both missed)
  - Result backfills L2 and L1 before returning
  - Full miss returns { valid: false } — no error, no retry
```

### What gets cached

- License records (keyed by tenant + license key)
- Tenant identity and API keys
- Product definitions
- Activation counts (short TTL — these change frequently)

The cache is a working set, not a full copy of the database. Entries are added on access, not preloaded at startup. This keeps memory bounded regardless of how many total licenses exist in the database.

### Cache consistency

Any write through the control plane immediately updates or invalidates the relevant cache entries. The flow is:

```
Admin write → PostgreSQL → cache update (write-through) or invalidate
```

Partial updates are avoided. The full object is always written or the key is invalidated and refetched. This prevents stale partial state.

In multi-node deployments, L1 invalidation events are propagated via Redis Pub/Sub so all instances evict stale entries without polling.

### TTL values

| Entry type | Default TTL | Notes |
|---|---|---|
| License (L1) | 5m | Short — licenses can be revoked |
| License (L2) | 72h | Long — Redis is the shared warm pool |
| Tenant | non-expiring (0) | Tenants mutate infrequently |
| Product | non-expiring (0) | Products mutate infrequently |
| Activation count | 30s | Short — seat counts change with every activation |
| Negative (not found) | 60s | Prevents hammering DB for invalid keys |

---

## Worker pool

The worker pool decouples HTTP request handling from validation execution. This has two benefits:

1. **Bounded concurrency** — a request spike does not create unbounded goroutines. The pool is fixed at `WORKER_COUNT` goroutines. If the queue fills up, requests are rejected cleanly with 503 instead of silently degrading.

2. **Timeout isolation** — each job runs with a context timeout. A slow database query on a cache miss cannot block the pool goroutine indefinitely. The context is cancelled after `WORKER_TIMEOUT`, the job returns an error, and the worker picks up the next job.

Panics inside workers are caught by a recovery wrapper, reported to the error reporter, and the worker restarts — the pool never silently shrinks.

---

## Signed offline licenses

For air-gapped environments and desktop applications, the service issues cryptographically signed license payloads. The client verifies the signature locally using a public key — no network call at verification time.

The signing algorithm is Ed25519. Each signed payload embeds a `kid` (key ID). The client fetches `/.well-known/jwks.json` once, finds the key matching `kid`, and verifies the signature against the `x` field. One fetch, one lookup, one verify — no branching.

The service supports two tiers of signing keys:

- **Global key** — used for all tenants by default. Loaded from `SIGNING_KEY_PATH` at startup.
- **Tenant override** — optional. A tenant registers their own Ed25519 key pair. All licenses for that tenant are signed with it. Useful for white-label products that need to issue licenses under their own brand key.

Both global and tenant public keys are served from the single `/.well-known/jwks.json` endpoint.

---

## Key rotation

API keys and signing keys can be rotated without downtime using a dual-key window.

During rotation, both the old and new key are valid. After the grace window closes, the old key is retired from the cache and database.

- **Tenant API key rotation**: `POST /admin/tenants/{id}/rotate-key` with `grace_period_minutes`
- **Global signing key rotation**: `POST /admin/signing-keys/rotate`
- **Tenant signing key rotation**: `POST /admin/tenants/{id}/signing-key/rotate`

Signed licenses issued before a signing key rotation remain verifiable — the old public key stays in the JWKS response until all previously issued payloads have passed their `expires_at`.

---

## Audit log

Every action that mutates state or accesses sensitive data is written to an append-only audit log table. The application user has no `UPDATE` or `DELETE` permission on this table.

Each entry records: actor ID, actor IP, event type, resource ID, outcome (success/failure), and a metadata blob for event-specific detail.

The audit log is queryable via `GET /admin/audit-log` with filters on tenant, event type, and time range.

---

## Webhook delivery

Webhooks are dispatched asynchronously, decoupled from the request lifecycle via a dedicated dispatcher goroutine. The main request does not wait for webhook delivery.

Delivery is retried with exponential backoff: 1s → 5s → 25s → 2m → 10m (up to 5 attempts). Failures are logged to the audit trail and queryable via the delivery history endpoint.

Payloads are signed with HMAC-SHA256 using the registered webhook secret. The signature is sent in `X-License-Signature`. Recipients must verify this header before processing.

Security constraints on webhook destinations:
- HTTPS only
- Private/loopback/link-local addresses blocked
- DNS rebinding protection (IP re-resolved at dial time)
- Redirects not followed
- 10s client timeout

---

## Graceful shutdown

On `SIGTERM` or `SIGINT`, the server shuts down in phases:

```
1. Stop accepting new HTTP connections
2. Close the job queue channel (no new jobs enqueued)
3. Wait for worker pool to drain (WORKER_DRAIN_TIMEOUT)
4. Flush audit log buffer
5. Flush error reporter
6. Flush OTEL span exporter
7. Close DB connection pool
8. Exit 0
```

In-flight requests complete. Queued jobs drain. Nothing is dropped silently.

---

## Layer responsibilities

| Layer | Location | Responsibility |
|---|---|---|
| Domain | `internal/domain/` | Pure structs and business rules. No DB, no HTTP. |
| Application | `internal/app/` | Use cases. Orchestrates domain and infrastructure via interfaces. |
| Infrastructure | `internal/infrastructure/` | PostgreSQL, LRU cache, Redis adapter, crypto, rate limiter. |
| HTTP | `internal/http/` | Fiber handlers, middleware, DTOs, router. |
| Worker | `internal/worker/` | Fixed goroutine pool, job queue, panic recovery. |
| Audit | `internal/audit/` | Async audit log writer. Cross-cutting concern. |
| Webhook | `internal/webhook/` | Dispatcher, retry scheduler, SSRF protection. |
| Server | `internal/server/` | Wires everything together, manages lifecycle. |

Infrastructure implements interfaces defined in the application layer. The application layer never imports infrastructure directly — only the interfaces. This keeps the domain and application layers testable without a database or cache.
