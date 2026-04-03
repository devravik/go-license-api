# Go License API

A high-performance, multi-tenant license validation service built with Go.

> Designed as a standalone validation microservice to offload heavy license checks from primary applications.

---

## Core Philosophy

Following the **Open Source** standard of clarity and explicitness:

- **Clarity over cleverness**: Code must be understandable in 30 seconds.
- **Explicit over implicit**: No hidden side effects or magic behavior.
- **Small focused components**: Single responsibility per file and package.
- **Predictable structure**: Standardized Go project layout (cmd/internal).
- **Public API stability**: Consistent and reliable integration patterns.

---

## Architecture & Flow

```text
Request → Tenant Auth → IP Check → Rate Limit → Queue → Worker → Validation → Audit Log → Response
```

### Pipelines

- **Data Plane (Runtime API)**: Handles high-performance validation requests via a layered cache (L1 in-memory LRU → L2 Redis → L3 PostgreSQL). Optimized for low-latency; the database is never touched on a hot-path cache hit.
- **Control Plane (Admin Layer)**: Manages tenants, API keys, rate limits, and key rotation independently. Isolated from the runtime pipeline to prevent management tasks from impacting validation performance.
- **Event Pipeline**: Fires webhook notifications for license events (expiry, activation failure, quota breach) asynchronously, decoupled from the request lifecycle.

### Lifecycle

1. **Ingest**: Received via Fiber HTTP API.
2. **IP Check**: Request source validated against tenant-level allowlist (admin endpoints: CIDR restriction).
3. **Resolve**: Tenant identity and API keys validated against the cache.
4. **Check**: Rate limits enforced via in-memory token bucket.
5. **Buffer**: Request pushed to a buffered channel (Job Queue).
6. **Execute**: Picked up by available workers with request timeout (context) handling. Workers handle both the fast path (L1/L2 cache hit) and the slow path (cache miss → DB fallback → cache backfill). Context cancellation prevents slow DB operations from blocking the pool.
7. **Audit**: Every action written to the immutable audit log (actor, IP, timestamp, outcome).
8. **Respond**: JSON result returned. Cache-hit latency is sub-millisecond; cache-miss latency is bounded by the configured handler timeout.
9. **Notify**: Webhook dispatcher fires asynchronously if the event matches a registered hook.

---

## Getting Started

### Prerequisites

- Go 1.21 or higher
- PostgreSQL 14+
- Docker (for local development)

### Admin CLI (Control Plane)

The project ships with a Cobra-based admin CLI that covers all control-plane domains (tenant, license, product, cache, system).

Quickstart:

```bash
# Build/run CLI directly
go run ./cmd/cli --pretty system config

# Examples
go run ./cmd/cli tenant create --rps=100 --burst=200 --pretty
go run ./cmd/cli license create --tenant=tenant_1 --key=ABC-123 --product=v-plugin --expires=2026-12-31 --meta='{"plan":"pro"}'
go run ./cmd/cli cache invalidate --tenant=tenant_1
```

CLI commands (examples):

```bash
# Tenant
go run ./cmd/cli tenant create --rps=100 --burst=200 --pretty
go run ./cmd/cli tenant update --id=tenant_1 --rps=200 --burst=400
go run ./cmd/cli tenant rotate-key --id=tenant_1 --grace=24h
go run ./cmd/cli tenant allowlist --id=tenant_1 --cidr=10.0.0.0/8 --cidr=192.168.0.0/16
go run ./cmd/cli tenant suspend --id=tenant_1 --reason="abuse"
go run ./cmd/cli tenant reinstate --id=tenant_1
go run ./cmd/cli tenant get --id=tenant_1
go run ./cmd/cli tenant list
go run ./cmd/cli tenant delete --id=tenant_1

# License
go run ./cmd/cli license create --tenant=tenant_1 --key=ABC-123 --product=v-plugin --expires=2026-12-31 --meta='{"plan":"pro"}'
go run ./cmd/cli license update --tenant=tenant_1 --key=ABC-123 --expires=2027-01-01
go run ./cmd/cli license revoke --tenant=tenant_1 --key=ABC-123
go run ./cmd/cli license get --tenant=tenant_1 --key=ABC-123
go run ./cmd/cli license list --tenant=tenant_1 --limit=100 --offset=0

# Product
go run ./cmd/cli product create --tenant=tenant_1 --id=v-plugin --name="Video Plugin" --version=1.0
go run ./cmd/cli product update --tenant=tenant_1 --id=v-plugin --name="Video Plugin Pro"
go run ./cmd/cli product get --tenant=tenant_1 --code=v-plugin
go run ./cmd/cli product list --tenant=tenant_1
go run ./cmd/cli product activate --tenant=tenant_1 --id=<product_id>
go run ./cmd/cli product deactivate --tenant=tenant_1 --id=<product_id>
go run ./cmd/cli product delete --tenant=tenant_1 --id=<product_id>

# Cache
go run ./cmd/cli cache invalidate --tenant=tenant_1
go run ./cmd/cli cache warmup --limit=500
go run ./cmd/cli cache reload --limit=500
go run ./cmd/cli cache reload --tenant=tenant_1
go run ./cmd/cli cache stats

# System
go run ./cmd/cli system health
go run ./cmd/cli system stats
go run ./cmd/cli system config
```

For deeper explanations and responses, see [development/16_cli_reference.md](development/16_cli_reference.md).

### Running Locally

```bash
# Clone repository
git clone https://github.com/devravik/go-license-api.git
cd go-license-api

# Start Postgres and dependencies
docker compose up -d

# Run database migrations (local)
# Uses DB_HOST/DB_PORT/DB_DATABASE/DB_USERNAME/DB_PASSWORD
go run ./cmd/migrate

# (Optional) Reset schema + re-run migrations (LOCAL ONLY)
go run ./cmd/migrate --refresh

# Install dependencies
go mod tidy

# Start the server
go run main.go
```

The server will be available at `http://localhost:8080`.

---

## Layered Cache Architecture

The service is designed for memory-constrained environments (512MB and above) and uses a layered caching strategy instead of preloading all licenses into memory, ensuring scalability without requiring large RAM footprints.

```text
┌────────────────────────────┐
│  L1: In-Memory LRU Cache   │  Always enabled. Bounded capacity.
│  (bounded, always on)      │  Evicts least-recently-used entries
└────────────┬───────────────┘  when the cap is reached.
             │ miss
             ▼
┌────────────────────────────┐
│  L2: Redis                 │  Optional. Shared across instances.
│  (optional, distributed)   │  Reduces DB pressure on multi-node
└────────────┬───────────────┘  deployments.
             │ miss
             ▼
┌────────────────────────────┐
│  L3: PostgreSQL            │  Source of truth. Queried only on
│  (source of truth)         │  a full cache miss. Result backfills
└────────────────────────────┘  L2 and L1 before returning.
```

### LicenseStore — Layered Lookup Abstraction

All validation passes through a single `LicenseStore` interface that encapsulates the layered behavior. Callers are unaware of which layer serves the response.

```go
// LicenseStore is the single access point for license data.
// It implements the L1 → L2 → L3 lookup chain internally.
type LicenseStore interface {
    Get(ctx context.Context, key string) (*License, error)
}
```

Internal lookup sequence:

```text
1. Check L1 (in-memory LRU)   → hit: return immediately
2. Check L2 (Redis, if on)    → hit: backfill L1, return
3. Query L3 (PostgreSQL)      → hit: backfill L2 + L1, return
                               → miss: return ErrLicenseNotFound
```

### Memory Constraints

The in-memory LRU cache is strictly bounded to prevent unbounded memory growth. The service does **not** preload all licenses at startup; the cache is populated on-demand based on access patterns.

| Parameter | Default | Notes |
|---|---|---|
| `CACHE_L1_MAX_ENTRIES` | `100000` | ~50–80MB for typical license records |
| `CACHE_LICENSE_TTL` | `5m` | Applies to both L1 and L2 |
| `CACHE_TENANT_TTL` | `10m` | Auth entries live longer; mutate less often |
| `CACHE_ACTIVATION_TTL` | `30s` | Short TTL — seat counts change frequently |

At 100,000 entries, the L1 cache stays well within 100MB, leaving the remaining memory budget for the worker pool, HTTP server, and runtime overhead.

### Redis (L2) — Optional

When `REDIS_URL` is set, Redis is enabled as L2. When absent, the service operates in single-node mode using L1 only with PostgreSQL as the direct fallback. There is no degraded mode — the layered store adapts at startup.

```env
REDIS_URL=redis://localhost:6379
REDIS_TLS=false
```

### Cache Invalidation

The cache uses a **TTL + event-driven invalidation** model. TTL is the safety net; event-driven invalidation ensures immediate consistency on critical mutations.

| Event | Scope invalidated |
|---|---|
| License revoked / updated | Single license key (L1 + L2) |
| Tenant suspended | All tenant keys (L1 + L2) |
| Tenant API key rotated | Tenant auth entry (L1 + L2) |
| Signing key retired | Public key set refreshed |

In distributed deployments (multiple instances), L1 invalidation events are propagated via **Redis Pub/Sub**, ensuring all instances evict stale entries promptly without polling.

```go
type Cache interface {
    Get(ctx context.Context, key string) (*CacheEntry, bool)
    Set(ctx context.Context, key string, value *CacheEntry, ttl time.Duration)
    Invalidate(ctx context.Context, scope, key string)
    InvalidateAll(ctx context.Context, scope string)
}
```

---



### Validate License

**Endpoint:** `POST /licenses/validate`

**Headers:**
- `X-Tenant-ID`: Identifies the tenant (required in multi-tenant mode).
- `X-API-Key`: Authentication key (required in multi-tenant mode).

**Request Body:**
```json
{
  "key": "ABC-123",
  "product": "v-plugin"
}
```

**Success Response:**
```json
{
  "valid": true,
  "meta": {
    "plan": "enterprise",
    "expires_at": "2025-12-31",
    "seats_total": 50,
    "seats_used": 12,
    "trial": false,
    "grace_period_ends_at": null,
    "features": ["sso", "audit-log", "custom-domain"]
  }
}
```

**Failure Response:**
```json
{
  "valid": false,
  "error": "license_expired",
  "grace_period_ends_at": "2025-01-14T00:00:00Z"
}
```

---

## Offline / Signed License Support

### Overview

Air-gapped environments and desktop applications require verifiable license files that can be validated without a network round-trip. The service issues cryptographically signed license payloads using **Ed25519** (preferred for compactness and speed) or **RS256 JWT** (for compatibility with existing toolchains).

Signed licenses are also the primary scalability escape hatch for high-throughput validation scenarios. Because verification uses only a public key — with no cache lookup, no Redis call, and no database query — they are ideal for:

- Desktop and native applications
- Air-gapped / offline environments
- High-scale validation where even L1 cache pressure is a concern

### Signing Key Strategy

The service supports two tiers of signing keys:

| Tier | Scope | When used |
|---|---|---|
| **Global key** | All tenants | Default. Used when no tenant-specific key is configured |
| **Tenant override** | Single tenant | Optional. Tenant registers their own Ed25519 key pair. All licenses for that tenant are signed with it |

This allows white-label SaaS products to issue licenses under their own brand key, completely decoupled from the platform's global key. A client verifying a tenant-issued license never needs to trust or even know about the platform's global key.

### Signer Resolution Flow

The complexity of "which key to use" is entirely server-side. The client never participates in signer selection.

```text
GET /licenses/{key}/signed
        │
        ▼
Resolve tenant from license key
        │
        ▼
Does tenant have a registered signing key?
   YES  │                    NO
        ▼                    ▼
  Tenant signer        Global signer
  (tenant's key)       (SIGNING_KEY_PATH)
        │                    │
        └─────────┬──────────┘
                  ▼
   Sign payload, embed kid (+ issuer for audit)
                  │
                  ▼
        Return signed license file
```

### Signed License Payload (Ed25519)

The payload embeds `kid` (key ID). The client uses `kid` to look up the correct public key in `/.well-known/jwks.json` — a direct map lookup with no branching. `issuer` is included for human readability and audit purposes only; it plays no role in the verification flow.

```json
{
  "license_key": "ABC-123",
  "product": "v-plugin",
  "tenant_id": "tenant_1",
  "plan": "enterprise",
  "expires_at": "2025-12-31T00:00:00Z",
  "seats": 50,
  "features": ["sso", "audit-log"],
  "issued_at": "2024-01-01T00:00:00Z",
  "kid": "tenant_1_key_01",
  "issuer": "tenant_1",
  "signature": "<base64-encoded-ed25519-signature>"
}
```

### Endpoints

**Issue a signed license file:**

`GET /licenses/{key}/signed`

Returns a signed JSON payload the client can cache locally and verify offline. The signer is resolved automatically — callers do not specify which key to use.

**Unified public key set (JWKS):**

`GET /.well-known/jwks.json`

Returns all active public keys — global and all tenant overrides — in a single JSON Web Key Set. The client reads `kid` from the signed payload and performs a direct lookup in this set. No branching, no endpoint selection.

```json
{
  "keys": [
    {
      "kid": "global_2024_01",
      "issuer": "global",
      "kty": "OKP",
      "crv": "Ed25519",
      "x": "<base64url-encoded-public-key>"
    },
    {
      "kid": "tenant_1_key_01",
      "issuer": "tenant_1",
      "kty": "OKP",
      "crv": "Ed25519",
      "x": "<base64url-encoded-public-key>"
    }
  ]
}
```

Keys within a rotation grace window are included in the set until all payloads signed by them have expired. Retired keys are removed automatically.

**Client verification flow:**

```text
Read kid from signed payload
        │
        ▼
GET /.well-known/jwks.json
        │
        ▼
Find key where keys[].kid == payload.kid
        │
        ▼
Verify signature using that key's x field
```

No issuer detection. No endpoint branching. One fetch, one lookup, one verify.

**Register a tenant signing key:**

`POST /admin/tenants/{id}/signing-key`

```json
{
  "public_key_pem": "-----BEGIN PUBLIC KEY-----\n...",
  "private_key_pem_encrypted": "<AES-256-GCM encrypted private key>",
  "passphrase_hint": "vault:secret/tenant_1/license_key"
}
```

**Remove a tenant signing key (revert to global):**

`DELETE /admin/tenants/{id}/signing-key`

Existing signed licenses issued under the tenant key remain verifiable until they expire — the public key is retained in the key set until all issued payloads have passed their `expires_at`.

### Key Material

- **Global private key**: Loaded from `SIGNING_KEY_PATH` (PEM, AES-256-GCM encrypted) at startup. Never leaves the server.
- **Tenant private keys**: Stored encrypted in PostgreSQL. Decrypted into memory on first use and cached for the process lifetime.
- **Key ID (`kid`)**: Embedded in every signed payload. The client uses `kid` as the sole lookup key into the JWKS response — no issuer detection, no endpoint branching.
- **Issuer (`issuer`)**: Embedded in the payload and in each JWKS entry for audit traceability. Not used by the verification algorithm itself.
- **Private key exposure**: Private keys are never returned via any API endpoint. Only public keys are exposed, exclusively through `/.well-known/jwks.json`.

### Schema Addition

```sql
CREATE TABLE tenant_signing_keys (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL UNIQUE REFERENCES tenants(id),
    kid TEXT NOT NULL,
    public_key_pem TEXT NOT NULL,
    private_key_encrypted TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    retired_at TIMESTAMP
);
```

### Implementation

```go
// LicenseSigner handles signing and verification for a single key pair.
type LicenseSigner interface {
    Sign(ctx context.Context, payload *LicensePayload) ([]byte, error)
    Verify(ctx context.Context, signed []byte) (*LicensePayload, error)
    PublicKeyPEM() []byte
    Kid() string
    Issuer() string // informational: "global" or tenant_id. Not used by verify logic.
}

// SignerRegistry resolves the correct LicenseSigner for a given tenant.
// Returns the tenant-specific signer if registered; falls back to the global signer.
type SignerRegistry interface {
    For(ctx context.Context, tenantID string) (LicenseSigner, error)
    Global() LicenseSigner
    RegisterTenant(ctx context.Context, tenantID string, keypair *KeyPair) error
    DeregisterTenant(ctx context.Context, tenantID string) error

    // JWKS returns all active public keys (global + all tenant overrides)
    // as a JSON Web Key Set. Powers GET /.well-known/jwks.json.
    JWKS(ctx context.Context) (*JWKSet, error)
}
```

`ValidationService` and `LicenseHandler` receive a `SignerRegistry`, not a bare `LicenseSigner`. The registry encapsulates the resolution logic; the rest of the system is unaware of whether a global or tenant key was used.

---

## Usage & Seat Tracking

### Overview

Supports the three most common commercial licensing models:

| Model | Description |
|---|---|
| Seat-based | Fixed number of named-user activations per license |
| Concurrent (float) | N users can be active simultaneously from any pool |
| Consumption-based | Usage units decremented per API call or event |

### Activation Flow

```text
Client calls /activate → Service checks seat availability → Grants or rejects → Records activation
```

### Endpoints

**Activate a seat:**

`POST /licenses/activate`

```json
{
  "key": "ABC-123",
  "product": "v-plugin",
  "machine_id": "sha256-of-hardware-fingerprint",
  "hostname": "workstation-42"
}
```

Response:
```json
{
  "activated": true,
  "activation_id": "act_abc123",
  "seats_remaining": 3
}
```

**Release a seat (concurrent/float model):**

`POST /licenses/deactivate`

```json
{
  "key": "ABC-123",
  "activation_id": "act_abc123"
}
```

**Report consumption:**

`POST /licenses/usage`

```json
{
  "key": "ABC-123",
  "units": 10
}
```

### Schema Additions

```sql
CREATE TABLE activations (
    id TEXT PRIMARY KEY,
    license_id INT NOT NULL REFERENCES licenses(id),
    tenant_id TEXT NOT NULL,
    machine_id TEXT,
    hostname TEXT,
    activated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMP,
    is_active BOOLEAN DEFAULT TRUE
);

CREATE TABLE usage_records (
    id SERIAL PRIMARY KEY,
    license_id INT NOT NULL REFERENCES licenses(id),
    tenant_id TEXT NOT NULL,
    units INT NOT NULL,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE licenses
    ADD COLUMN seat_count INT DEFAULT NULL,
    ADD COLUMN max_activations INT DEFAULT NULL,
    ADD COLUMN usage_limit INT DEFAULT NULL,
    ADD COLUMN usage_used INT DEFAULT 0;
```

---

## Trial & Grace Period Model

### Overview

Supports time-limited trial licenses and configurable grace periods after expiry. Feature gating is enforced at the validation layer based on the license plan.

### License States

```text
trial → active → grace_period → expired
```

| State | Validation result | Notes |
|---|---|---|
| `trial` | `valid: true` | `trial: true` flag returned |
| `active` | `valid: true` | Normal operation |
| `grace_period` | `valid: true` | `grace_period_ends_at` returned; client should warn user |
| `expired` | `valid: false` | `error: license_expired` |

### Schema Additions

```sql
ALTER TABLE licenses
    ADD COLUMN is_trial BOOLEAN DEFAULT FALSE,
    ADD COLUMN trial_ends_at TIMESTAMP,
    ADD COLUMN grace_period_days INT DEFAULT 0,
    ADD COLUMN grace_period_ends_at TIMESTAMP GENERATED ALWAYS AS (expires_at + (grace_period_days || ' days')::INTERVAL) STORED;
```

### Feature Gating

Plan-to-feature mappings are stored per tenant and returned in validation responses, allowing clients to gate features locally without additional API calls.

```go
type FeatureSet struct {
    Plan     string   `json:"plan"`
    Features []string `json:"features"`
}
```

---

## Key Rotation

### Overview

API keys and signing keys can be rotated without service downtime using a **dual-key window** strategy. During the rotation window, both the old and new keys are accepted. The old key is retired only after the window closes.

### API Key Rotation (Tenant Auth Keys)

`POST /admin/tenants/{id}/rotate-key`

```json
{
  "grace_period_minutes": 60
}
```

Response:
```json
{
  "new_api_key": "new_key_abc",
  "old_key_expires_at": "2024-01-01T02:00:00Z"
}
```

During the grace window, both the old and new key are valid. After expiry, the old key is removed from the cache and database.

### Signing Key Rotation — Global Key

Rotates the platform-wide signing key used for all tenants that do not have a tenant-specific override.

`POST /admin/signing-keys/rotate`

- Generates a new Ed25519 key pair.
- Assigns a new `kid`.
- Retains the old public key in `/.well-known/license-pubkey.pem` (as a key set) until all previously issued signed payloads have passed their `expires_at`.
- The retired private key is securely destroyed from memory and storage.

### Signing Key Rotation — Tenant Override Key

Rotates the signing key for a specific tenant. Follows the same dual-key window pattern.

`POST /admin/tenants/{id}/signing-key/rotate`

```json
{
  "private_key_pem_encrypted": "<new encrypted private key>",
  "public_key_pem": "-----BEGIN PUBLIC KEY-----\n...",
  "grace_period_hours": 48
}
```

- The new key is registered with a new `kid`.
- The old public key remains in `/.well-known/tenants/{tenant_id}/license-pubkey.pem` for the grace period.
- After the grace window, the old private key is destroyed and the old public key is removed from the set.
- Signed licenses issued before rotation continue to verify against the old public key until they expire naturally.

### Rotation Decision Matrix

| Scenario | Endpoint |
|---|---|
| Rotate platform key for all tenants using global default | `POST /admin/signing-keys/rotate` |
| Rotate a specific tenant's custom key | `POST /admin/tenants/{id}/signing-key/rotate` |
| Switch a tenant from global key to their own key | `POST /admin/tenants/{id}/signing-key` |
| Switch a tenant back to the global key | `DELETE /admin/tenants/{id}/signing-key` |

### Implementation

```go
type KeyRotator interface {
    // Rotates the tenant authentication API key.
    RotateTenantKey(ctx context.Context, tenantID string, gracePeriod time.Duration) (*RotationResult, error)

    // Rotates the global platform signing key.
    RotateGlobalSigningKey(ctx context.Context) (*SigningKeyPair, error)

    // Rotates a tenant-specific signing key override.
    RotateTenantSigningKey(ctx context.Context, tenantID string, newKeypair *KeyPair, gracePeriod time.Duration) (*RotationResult, error)

    // Retires an old key after its grace window expires.
    RetireKey(ctx context.Context, kid string) error
}

---

## Audit Log

### Overview

Every action that mutates state or accesses sensitive data is written to an immutable audit log. This satisfies compliance requirements (SOC 2, ISO 27001, GDPR) and provides a forensic trail for fraud investigation.

### Logged Events

| Category | Events |
|---|---|
| License | validate, create, update, revoke, activate, deactivate |
| Tenant | create, suspend, delete, key-rotate |
| Admin | login, action, key-rotate |
| Security | auth-failure, rate-limit-breach, ip-block |

### Audit Record

```go
type AuditEntry struct {
    ID         string    `json:"id"`
    TenantID   string    `json:"tenant_id"`
    ActorID    string    `json:"actor_id"`    // admin key ID or tenant key ID
    ActorIP    string    `json:"actor_ip"`
    Event      string    `json:"event"`
    ResourceID string    `json:"resource_id"` // license key, tenant ID, etc.
    Outcome    string    `json:"outcome"`     // "success" | "failure"
    Meta       JSONB     `json:"meta"`        // event-specific detail
    CreatedAt  time.Time `json:"created_at"`
}
```

### Schema

```sql
CREATE TABLE audit_log (
    id TEXT PRIMARY KEY,
    tenant_id TEXT,
    actor_id TEXT NOT NULL,
    actor_ip TEXT NOT NULL,
    event TEXT NOT NULL,
    resource_id TEXT,
    outcome TEXT NOT NULL,
    meta JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_audit_tenant ON audit_log(tenant_id, created_at DESC);
CREATE INDEX idx_audit_event ON audit_log(event, created_at DESC);
```

Audit records are append-only. No `UPDATE` or `DELETE` permissions are granted to the application user on this table.

### Query Endpoint

`GET /admin/audit-log?tenant_id=&event=&from=&to=&limit=`

---

## IP Allowlisting

### Overview

Two layers of IP-based access control protect the service from brute-force and enumeration attacks.

### Admin Endpoint CIDR Restriction

All `/admin/*` routes are restricted to a configurable CIDR list. Requests from outside the allowed ranges receive `403 Forbidden` before authentication is attempted.

```env
ADMIN_ALLOWED_CIDRS=10.0.0.0/8,172.16.0.0/12
```

### Tenant-Level IP Allowlisting

Each tenant can specify a list of allowed IPs or CIDR blocks. Validation requests from unlisted IPs are rejected with `403` and logged to the audit trail.

`POST /admin/tenants/{id}/ip-allowlist`

```json
{
  "cidrs": ["203.0.113.0/24", "198.51.100.42/32"]
}
```

### Schema Addition

```sql
ALTER TABLE tenants ADD COLUMN ip_allowlist TEXT[] DEFAULT NULL;
```

A `NULL` allowlist means all IPs are permitted (default, backward compatible). An empty array `[]` blocks all requests, useful for suspended tenants.

### Implementation

```go
type IPGuard interface {
    CheckAdminCIDR(ip net.IP) bool
    CheckTenantAllowlist(tenantID string, ip net.IP) bool
}
```

---

## Distributed Tracing

### Overview

All service operations are instrumented with **OpenTelemetry** (OTEL) spans. Traces propagate across the HTTP layer, job queue, and worker pool, enabling end-to-end visibility into slow or failing requests.

### Instrumented Layers

| Layer | Spans |
|---|---|
| HTTP | Incoming request, middleware chain |
| Auth | Tenant resolve, API key check |
| Rate Limiter | Token bucket check |
| Job Queue | Enqueue, dequeue, queue depth |
| Worker | Job execution, timeout |
| Cache | Hit, miss, invalidation |
| Database | Query execution (pgxotel) |
| Webhook | Dispatch, retry |

### Configuration

```env
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
OTEL_SERVICE_NAME=go-license-api
OTEL_TRACE_SAMPLE_RATE=1.0
```

### Implementation

```go
// Trace context propagated through the worker via context.Context.
// No global state; spans created and closed in the same function scope.

func (w *Worker) process(ctx context.Context, job Job) {
    ctx, span := tracer.Start(ctx, "worker.process")
    defer span.End()
    // ...
}
```

---

## Structured Error Reporting

### Overview

Unhandled errors and panics in goroutines are captured and forwarded to a configurable error aggregation backend (Sentry by default, extensible to Rollbar or custom webhooks).

### Goroutine Safety

All worker goroutines wrap execution in a recovery block. Panics are caught, converted to structured errors, reported, and the worker is restarted — the pool never silently dies.

```go
func (w *Worker) safeRun(ctx context.Context, job Job) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
            errorReporter.Capture(ctx, err)
        }
    }()
    return w.process(ctx, job)
}
```

### Configuration

```env
ERROR_REPORTER=sentry                        # sentry | rollbar | none
SENTRY_DSN=https://key@sentry.io/project-id
```

### Error Reporter Interface

```go
type ErrorReporter interface {
    Capture(ctx context.Context, err error, extras ...map[string]any)
    CaptureMessage(ctx context.Context, msg string, level string)
    Flush(timeout time.Duration)
}
```

---

## Graceful Shutdown

### Overview

The service handles `SIGTERM` and `SIGINT` with a coordinated multi-phase shutdown, ensuring in-flight requests complete and queued jobs drain before the process exits.

### Shutdown Sequence

```text
Signal received
  → Stop accepting new HTTP connections (Fiber shutdown with timeout)
  → Close job queue channel (no new jobs enqueued)
  → Wait for worker pool to drain (with drain timeout)
  → Flush audit log buffer
  → Flush error reporter
  → Flush OTEL span exporter
  → Close DB connection pool
  → Exit 0
```

### Configuration

```env
SHUTDOWN_TIMEOUT=30s        # Total graceful window
WORKER_DRAIN_TIMEOUT=25s    # Max time to wait for workers
```

### Implementation

```go
func (s *Server) handleShutdown(ctx context.Context) {
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
    <-quit

    ctx, cancel := context.WithTimeout(ctx, s.cfg.ShutdownTimeout)
    defer cancel()

    s.app.ShutdownWithContext(ctx)    // stop accepting HTTP
    s.queue.Close()                   // stop enqueuing
    s.workerPool.Drain(ctx)           // wait for workers
    s.auditWriter.Flush()
    s.errorReporter.Flush(5 * time.Second)
    s.traceProvider.Shutdown(ctx)
    s.db.Close()
}
```

---

## Tenant Suspension & GDPR

### Tenant Suspension

Tenants can be suspended without deletion. Suspended tenants have all validation requests rejected at the middleware layer (no worker or DB involvement) and their cache entries invalidated immediately.

`POST /admin/tenants/{id}/suspend`

`POST /admin/tenants/{id}/reinstate`

### Schema Addition

```sql
ALTER TABLE tenants
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN suspended_at TIMESTAMP,
    ADD COLUMN suspension_reason TEXT;
```

Status values: `active | suspended | deleted`.

### GDPR: Right to Erasure

`DELETE /admin/tenants/{id}`

Soft-deletes the tenant record and schedules a background job to:

1. Anonymize personally identifiable fields (email, IP addresses) in the audit log (audit events are retained; PII is scrubbed).
2. Permanently delete all license, activation, and usage records.
3. Remove the tenant from the in-memory cache.
4. Return a deletion receipt with a job ID the caller can poll.

Audit log entries for the deletion event itself are retained permanently and are not subject to erasure.

---

## Webhook / Event System

### Overview

Tenants register webhook endpoints. The service dispatches signed HTTP POST payloads asynchronously when license events occur, decoupled from the request lifecycle via a dedicated dispatcher goroutine.

### Supported Events

| Event | Fired when |
|---|---|
| `license.validated` | A license is successfully validated |
| `license.validation_failed` | Validation fails (wrong key, expired, etc.) |
| `license.expired` | A license crosses its expiry timestamp (scheduled check) |
| `license.grace_period_started` | License enters grace period |
| `license.activated` | A seat is activated |
| `license.deactivated` | A seat is released |
| `license.revoked` | Admin revokes a license |
| `quota.exceeded` | Rate limit or seat count breached |
| `tenant.suspended` | Tenant is suspended |

### Webhook Registration

`POST /admin/tenants/{id}/webhooks`

```json
{
  "url": "https://your-app.com/hooks/license",
  "events": ["license.expired", "license.activated"],
  "secret": "your-signing-secret"
}
```

### Delivery Security Constraints

To prevent SSRF and related attacks, webhook delivery is locked down:

- HTTPS-only: non-HTTPS URLs are rejected at registration and dispatch
- Private/loopback/link-local blocked: `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.0.0/16`, IPv6 loopback/ULA
- DNS rebinding protection: destination IP is re-resolved at dial time and re-checked
- Redirects disabled: 3xx responses are not automatically followed
- Timeouts: 10s client timeout; connection/TLS timeouts are shorter

If a URL violates these rules, registration returns `400 invalid_webhook_url` and dispatches are skipped with a logged notice.

### Payload

```json
{
  "id": "evt_abc123",
  "event": "license.expired",
  "tenant_id": "tenant_1",
  "occurred_at": "2025-01-01T00:00:00Z",
  "data": {
    "license_key": "ABC-123",
    "product": "v-plugin",
    "expired_at": "2024-12-31T23:59:59Z"
  }
}
```

The payload is signed using HMAC-SHA256 with the registered secret. The signature is delivered in the `X-License-Signature` header. Recipients must verify this header before processing.

#### Delivery headers and limits

- `X-Webhook-Version: v1`
- `X-Webhook-Id: <event-id>`
- `X-Webhook-Attempt: <1..5>`
- `X-Body-SHA256: <hex>`
- `X-License-Timestamp: <unix-seconds>`
- `X-License-Signature: v1=<hex-hmac-sha256>`
- Retries: 1s → 5s → 25s → 2m → 10m (non-2xx only)
- Max payload size: 256KB (larger payloads are dropped and logged)
- Ordering: not guaranteed; rely on `occurred_at` and idempotency on receiver

### Reliability

Webhook dispatches are retried with exponential back-off (up to 5 attempts, max 1 hour window). Failed deliveries are logged to the audit trail. Delivery attempts and outcomes are queryable via:

`GET /admin/tenants/{id}/webhooks/{webhook_id}/deliveries`

### Schema

```sql
CREATE TABLE webhooks (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    url TEXT NOT NULL,
    events TEXT[] NOT NULL,
    secret_hash TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webhook_deliveries (
    id TEXT PRIMARY KEY,
    webhook_id TEXT NOT NULL REFERENCES webhooks(id),
    event TEXT NOT NULL,
    payload JSONB NOT NULL,
    attempt INT DEFAULT 1,
    status TEXT NOT NULL,       -- pending | success | failed
    response_code INT,
    next_retry_at TIMESTAMP,
    delivered_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

---

## OpenAPI Specification

### Overview

The service ships a machine-readable **OpenAPI 3.1** specification as a first-class artifact. It is auto-generated from annotated handler code (using `swaggo/swag` or `go-swagger`) and served at runtime.

### Endpoints

| Path | Description |
|---|---|
| `GET /docs/openapi.yaml` | Raw OpenAPI 3.1 spec |
| `GET /docs` | Swagger UI (development only; disabled in production via `APP_ENV`) |

### Generation

```bash
# Install swag
go install github.com/swaggo/swag/cmd/swag@latest

# Generate spec from annotations
swag init -g cmd/server/main.go -o docs/
```

### Usage

The spec enables:

- **SDK generation** via `openapi-generator` for any target language (Node, Python, PHP, etc.).
- **Postman collection** import directly from the spec URL.
- **Contract testing** using `dredd` or `schemathesis` in CI.
- **Mock server** for consumer-driven testing during development.

---

## Public API Design

```bash
go get github.com/devravik/go-license-api
```

```go
validator := license.NewValidator(cache)
result, err := validator.Validate(ctx, input)
if err != nil {
    // handle error
}
```

---

## Tech Stack

- **Backend**: Go (Golang) with the Fiber web framework.
- **Concurrency**: Goroutines and buffered channels for the worker pool implementation.
- **Storage**: PostgreSQL (production) using lightweight SQL or query builders.
- **Caching**: Layered — L1 bounded in-memory LRU; L2 Redis (optional, for distributed deployments); L3 PostgreSQL fallback.
- **Rate Limiting**: Native Go token bucket implementation for per-tenant control.
- **Crypto**: Ed25519 for license signing; HMAC-SHA256 for webhook payloads.
- **Tracing**: OpenTelemetry (OTLP exporter).
- **Error Reporting**: Sentry SDK (pluggable via `ErrorReporter` interface).
- **Logging**: Fiber Logger with `lumberjack` for daily rotation and Request ID tracking.

---

## Project Structure

```text
.
├── cmd/
│   └── server/                # Entrypoint (main.go)
│
├── internal/                  # Private application logic
│   ├── app/                   # Use cases (ValidationService, ActivationService, WebhookDispatcher)
│   ├── domain/                # Business models and rules
│   │   ├── license.go         # License, ActivationRecord, UsageRecord
│   │   ├── tenant.go          # Tenant, IPAllowlist
│   │   ├── audit.go           # AuditEntry
│   │   └── webhook.go         # Webhook, WebhookDelivery
│   ├── http/                  # HTTP Transport layer
│   │   ├── handlers/          # Thin route handlers
│   │   ├── middleware/        # Tenant, Auth, IP Guard, Rate limit middlewares
│   │   ├── dto/               # API Request/Response models
│   │   └── router.go          # Route registration & grouping
│   ├── server/                # Application orchestrator (Server/Fiber setup, graceful shutdown)
│   ├── infrastructure/        # DB, Cache, External integrations
│   │   ├── cache/
│   │   │   ├── lru.go         # L1 in-memory LRU (bounded)
│   │   │   ├── redis.go       # L2 Redis adapter (optional)
│   │   │   └── store.go       # LicenseStore: L1→L2→L3 chain
│   │   ├── crypto/
│   │   │   ├── signer.go      # LicenseSigner — single key pair (Ed25519)
│   │   │   ├── registry.go    # SignerRegistry — global default + tenant override resolution
│   │   │   └── rotator.go     # KeyRotator — global and per-tenant rotation
│   │   └── reporting/         # ErrorReporter (Sentry adapter)
│   ├── audit/                 # Audit log writer and query service
│   ├── webhook/               # Dispatcher, retry scheduler
│   └── worker/                # Background job processing
│
├── pkg/                       # Reusable public utilities
├── configs/                   # Unified configuration (main & logging)
├── docs/                      # OpenAPI spec (auto-generated) + guides
├── migrations/                # Database migrations
├── tests/                     # Unit and integration tests
├── deployments/
│   ├── docker/
│   │   ├── Dockerfile
│   │   └── docker-compose.yml
│   └── k8s/
│       ├── deployment.yaml
│       ├── service.yaml
│       ├── configmap.yaml
│       └── helm/
├── README.md
└── go.mod
```

---

## Layer Responsibilities

- **Domain**: Pure structs and business rules (e.g., `IsExpired()`, `IsInGracePeriod()`, `HasFeature()`). Strictly no DB or HTTP dependencies.
- **Application**: Orchestration layer. Executes use cases and interacts with domain/infrastructure interfaces.
- **Infrastructure**: Implementation of storage (PostgreSQL), caching (with TTL + invalidation), cryptographic signing, and error reporting.
- **Transport (HTTP)**: Fiber handlers and middleware. Responsible for request parsing, IP guard enforcement, and response formatting.
- **Audit**: Cross-cutting concern. Written to from Application and Transport layers via the `AuditWriter` interface; never from Domain.

### Interfaces

```go
type LicenseRepository interface {
    FindByKey(ctx context.Context, key string) (*License, error)
    Create(ctx context.Context, l *License) error
    Revoke(ctx context.Context, key string) error
}

// SignerRegistry resolves the correct LicenseSigner per tenant.
// Falls back to the global signer when no tenant override is registered.
type SignerRegistry interface {
    For(ctx context.Context, tenantID string) (LicenseSigner, error)
    Global() LicenseSigner
    RegisterTenant(ctx context.Context, tenantID string, keypair *KeyPair) error
    DeregisterTenant(ctx context.Context, tenantID string) error
    PublicKeySet(ctx context.Context, tenantID string) ([]*PublicKeyEntry, error)
}

// LicenseSigner handles signing and verification for a single key pair.
type LicenseSigner interface {
    Sign(ctx context.Context, payload *LicensePayload) ([]byte, error)
    Verify(ctx context.Context, signed []byte) (*LicensePayload, error)
    PublicKeyPEM() []byte
    Kid() string
    Issuer() string
}

type ActivationRepository interface {
    Activate(ctx context.Context, record *ActivationRecord) error
    Release(ctx context.Context, activationID string) error
    CountActive(ctx context.Context, licenseID int) (int, error)
}

type AuditWriter interface {
    Write(ctx context.Context, entry *AuditEntry)
    Flush()
}

type WebhookDispatcher interface {
    Dispatch(ctx context.Context, event string, tenantID string, data any)
}

type ErrorReporter interface {
    Capture(ctx context.Context, err error, extras ...map[string]any)
    Flush(timeout time.Duration)
}

// LicenseStore is the single access point for license data.
// Implements the L1 LRU → L2 Redis → L3 PostgreSQL lookup chain.
// Callers are unaware of which layer served the response.
type LicenseStore interface {
    Get(ctx context.Context, key string) (*License, error)
}

// Cache is the interface for a single cache layer (L1 or L2).
// LicenseStore composes two Cache implementations internally.
type Cache interface {
    Get(ctx context.Context, key string) (*CacheEntry, bool)
    Set(ctx context.Context, key string, value *CacheEntry, ttl time.Duration)
    Invalidate(ctx context.Context, scope, key string)
    InvalidateAll(ctx context.Context, scope string)
}

type IPGuard interface {
    CheckAdminCIDR(ip net.IP) bool
    CheckTenantAllowlist(tenantID string, ip net.IP) bool
}
```

---

## Standards

### Naming Conventions

- **Files**: `snake_case` (e.g., `tenant_service.go`, `license_validator.go`).
- **Interfaces**: Explicit naming (e.g., `type TenantRepository interface {}`).
- **Constructors**: Use `New` prefix (e.g., `func NewTenantService(repo TenantRepository)`).

### Code Style

- **Small functions**: Functions should generally be under 50 lines.
- **Dependency Injection**: Use constructor-based DI; avoid globals and hidden side effects.
- **No Shared State**: Avoid shared mutable state without proper synchronization.

### Error Handling

Use typed and structured errors for predictability:

```go
var (
    ErrLicenseExpired     = errors.New("license expired")
    ErrLicenseRevoked     = errors.New("license revoked")
    ErrLicenseGracePeriod = errors.New("license in grace period")
    ErrSeatLimitReached   = errors.New("seat limit reached")
    ErrInvalidTenant      = errors.New("invalid tenant")
    ErrTenantSuspended    = errors.New("tenant suspended")
    ErrIPNotAllowed       = errors.New("ip not in allowlist")
    ErrKeyExpired         = errors.New("api key expired")
)
```

### Testing Standard

- **Unit (Domain)**: 100% testable logic without dependencies.
- **Integration (Infra)**: Validating database and cache implementations, including invalidation behavior.
- **Mocking**: Used at the Application layer to isolate use cases from infrastructure.
- **Contract Testing**: `schemathesis` runs against the OpenAPI spec in CI to catch handler/spec drift.
- **Security Scanning**: `gosec` and `govulncheck` run on every PR.

---

## Configuration

The service utilizes a singleton configuration pattern: **Load once, inject everywhere.**

### Configuration Pattern

1. **Load**: Configuration is loaded from environment variables or files at startup.
2. **Inject**: The `Config` struct is injected into handlers and services via constructors.

### Environment Variables

**Application**

| Variable | Default | Description |
|---|---|---|
| `APP_NAME` | `Go License API` | Name of the application |
| `APP_PORT` | `3000` | Server port |
| `APP_MODE` | `multi` | Deployment mode: `single` or `multi` |
| `APP_ENV` | `production` | `development` enables Swagger UI |

**Security**

| Variable | Default | Description |
|---|---|---|
| `ADMIN_API_KEY` | — | Secret key for administrative operations |
| `ADMIN_ALLOWED_CIDRS` | — | Comma-separated CIDR list for admin endpoint restriction |
| `SIGNING_KEY_PATH` | — | Path to Ed25519 private key file (PEM, encrypted) |
| `SIGNING_KEY_PASSPHRASE` | — | Passphrase to decrypt the signing key at startup |

**Shutdown**

| Variable | Default | Description |
|---|---|---|
| `SHUTDOWN_TIMEOUT` | `30s` | Total graceful shutdown window |
| `WORKER_DRAIN_TIMEOUT` | `25s` | Maximum drain time for worker pool |

**Cache**

| Variable | Default | Description |
|---|---|---|
| `CACHE_L1_MAX_ENTRIES` | `100000` | L1 LRU capacity. ~80MB at default. Hard ceiling — evicts LRU entries when reached |
| `CACHE_LICENSE_TTL` | `5m` | TTL for license records (L1 and L2) |
| `CACHE_TENANT_TTL` | `10m` | TTL for tenant auth entries |
| `CACHE_ACTIVATION_TTL` | `30s` | TTL for activation count (short — changes frequently) |
| `REDIS_URL` | — | Redis connection URL. L2 cache disabled when unset |
| `REDIS_TLS` | `false` | Enable TLS for Redis connection |

**Observability**

| Variable | Default | Description |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP collector endpoint |
| `OTEL_SERVICE_NAME` | `go-license-api` | Service name in traces |
| `OTEL_TRACE_SAMPLE_RATE` | `1.0` | Trace sampling rate (0.0–1.0) |
| `ERROR_REPORTER` | `none` | `sentry` or `none` |
| `SENTRY_DSN` | — | Sentry DSN |

**Logging**

| Variable | Default | Description |
|---|---|---|
| `LOG_ENABLED` | `true` | Enable or disable request logging |
| `LOG_OUTPUT` | `stdout` | `stdout` or `file` |
| `LOG_DIR` | `./logs` | Directory for log files |
| `LOG_LEVEL` | `error` | Log level |

---

## Deployment

### Docker

```bash
# Build image
docker build -f deployments/docker/Dockerfile -t go-license-api .

# Start with Postgres
docker compose -f deployments/docker/docker-compose.yml up
```

### Kubernetes

```bash
# Apply manifests
kubectl apply -f deployments/k8s/

# Or via Helm
helm install go-license-api deployments/k8s/helm/
```

### Health Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /healthz` | Liveness — process is alive |
| `GET /readyz` | Readiness — cache warmed, DB connected |

---

## Performance & Security

### Performance

- **Cache-Optimized Validation**: Validation requests are served from the layered cache (L1 LRU → L2 Redis → L3 PostgreSQL). Database access only occurs on a full cache miss; the hot path never touches PostgreSQL.
- **Signed License Fast Path**: Signed license verification bypasses all cache and database layers entirely — verified locally via public key. Zero network or storage overhead.
- **Bounded Memory**: The L1 LRU cache is strictly capped (default 100,000 entries, ~80MB). The service does not preload the full dataset and remains viable in 512MB environments.
- **TTL + Event Invalidation**: Cache stays consistent without polling. Distributed invalidation via Redis Pub/Sub in multi-node deployments.
- **Worker Isolation**: Background processing decoupled from the request-response cycle. Workers handle both fast-path (cache hit) and slow-path (cache miss + DB) validations, with context-based timeouts preventing slow DB operations from stalling the pool.
- **Native Efficiency**: Built on Go's high-performance concurrency primitives.

### Timeout Architecture

**Dual-Layer Timeout**:

- **Server Layer (TCP)**: Managed in `fiber.Config` to prevent connection-level resource exhaustion.
- **Handler Layer (Context)**: Enforced via `timeout` middleware on each route, triggering a clean `408` JSON response and propagating cancellation to stop resource-heavy operations.

### Security Summary

| Control | Implementation |
|---|---|
| Admin CIDR restriction | Middleware; configurable via `ADMIN_ALLOWED_CIDRS` |
| Tenant IP allowlisting | Middleware; stored in cache + DB |
| API key rotation | Dual-key grace window |
| Global signing key rotation | `kid`-based multi-key window; all keys served via `/.well-known/jwks.json` |
| Tenant signing key override | Optional per-tenant Ed25519 key pair; public key included in unified JWKS |
| Tenant signing key rotation | Same dual-key window; retired public keys remain in JWKS until all issued payloads expire |
| Private key exposure | Private keys never returned by any API. Only public keys exposed via `/.well-known/jwks.json` |
| Webhook payload auth | HMAC-SHA256 signature header |
| Webhook destination restrictions | HTTPS-only; private/loopback blocked; DNS-rebinding protection; redirects disabled; timeouts |
| Audit log integrity | Append-only; no app-level DELETE permission |

---

## Logging Architecture

- **Fiber Logger**: Captures every request with timestamp, status, method, path, and Request ID.
- **Log Rotation**: Uses `lumberjack` for automatic daily rotation, compression, and retention management.
- **Fail-Safe Output**: Defaults to `stdout` for containerized environments (Docker/K8s).
- **Request ID Tracking**: Every log entry includes a unique request ID for end-to-end tracing.
- **OTEL Correlation**: Trace ID and span ID included in log fields when tracing is enabled.

---

## Database & Migrations

Migrations are stored in the `/migrations/` directory as incremental SQL scripts.

### Core Schema

```sql
CREATE TABLE tenants (
    id TEXT PRIMARY KEY,
    api_key TEXT NOT NULL,
    rps INT DEFAULT 100,
    burst INT DEFAULT 200,
    status TEXT NOT NULL DEFAULT 'active',
    suspended_at TIMESTAMP,
    suspension_reason TEXT,
    ip_allowlist TEXT[] DEFAULT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE licenses (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    key TEXT NOT NULL,
    product_id TEXT,
    product TEXT,
    status TEXT,
    plan TEXT,
    is_trial BOOLEAN DEFAULT FALSE,
    trial_ends_at TIMESTAMP,
    expires_at TIMESTAMP,
    grace_period_days INT DEFAULT 0,
    seat_count INT DEFAULT NULL,
    max_activations INT DEFAULT NULL,
    usage_limit INT DEFAULT NULL,
    usage_used INT DEFAULT 0,
    meta JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE activations (
    id TEXT PRIMARY KEY,
    license_id INT NOT NULL REFERENCES licenses(id),
    tenant_id TEXT NOT NULL,
    machine_id TEXT,
    hostname TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    activated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMP
);

CREATE TABLE usage_records (
    id SERIAL PRIMARY KEY,
    license_id INT NOT NULL REFERENCES licenses(id),
    tenant_id TEXT NOT NULL,
    units INT NOT NULL,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE audit_log (
    id TEXT PRIMARY KEY,
    tenant_id TEXT,
    actor_id TEXT NOT NULL,
    actor_ip TEXT NOT NULL,
    event TEXT NOT NULL,
    resource_id TEXT,
    outcome TEXT NOT NULL,
    meta JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webhooks (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    url TEXT NOT NULL,
    events TEXT[] NOT NULL,
    secret_hash TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webhook_deliveries (
    id TEXT PRIMARY KEY,
    webhook_id TEXT NOT NULL REFERENCES webhooks(id),
    event TEXT NOT NULL,
    payload JSONB NOT NULL,
    attempt INT DEFAULT 1,
    status TEXT NOT NULL,
    response_code INT,
    next_retry_at TIMESTAMP,
    delivered_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_license_key ON licenses(key);
CREATE INDEX idx_license_tenant ON licenses(tenant_id);
CREATE INDEX idx_activation_license ON activations(license_id) WHERE is_active = TRUE;
CREATE INDEX idx_audit_tenant ON audit_log(tenant_id, created_at DESC);
CREATE INDEX idx_audit_event ON audit_log(event, created_at DESC);
CREATE INDEX idx_webhook_tenant ON webhooks(tenant_id);
CREATE INDEX idx_delivery_retry ON webhook_deliveries(next_retry_at) WHERE status = 'pending';

CREATE TABLE tenant_signing_keys (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL UNIQUE REFERENCES tenants(id),
    kid TEXT NOT NULL,
    public_key_pem TEXT NOT NULL,
    private_key_encrypted TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    retired_at TIMESTAMP
);

CREATE INDEX idx_signing_key_tenant ON tenant_signing_keys(tenant_id) WHERE is_active = TRUE;
```

### Running Migrations

```bash
psql -U postgres -d your_db -f migrations/001_init.sql
```

---

## Deployment Modes

### Mode Selection

`APP_MODE=single` — Optimized for standalone or self-hosted setups. No tenant headers required.

`APP_MODE=multi` — SaaS platforms requiring strict data and resource isolation.

### Tenant Resolution Lifecycle

1. **Request Intake**: Middleware identifies the current deployment mode.
2. **IP Guard**: Request IP validated against admin CIDR list or tenant allowlist.
3. **Contextual Resolution**: Single mode assigns default tenant; multi mode validates provided headers.
4. **Suspension Check**: Suspended tenant requests are rejected before entering the worker pipeline.
5. **Scoped Processing**: Storage layer executes queries filtered by resolved tenant identity.

---

## Admin APIs

### Security

All `/admin/*` routes require:

1. `X-Admin-Key` header matching `ADMIN_API_KEY`.
2. Source IP within `ADMIN_ALLOWED_CIDRS` (if configured).

### Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/tenants` | Create tenant |
| `POST` | `/admin/tenants/{id}/suspend` | Suspend tenant |
| `POST` | `/admin/tenants/{id}/reinstate` | Reinstate tenant |
| `DELETE` | `/admin/tenants/{id}` | GDPR delete |
| `POST` | `/admin/tenants/{id}/rotate-key` | Rotate tenant auth API key |
| `POST` | `/admin/tenants/{id}/ip-allowlist` | Set IP allowlist |
| `POST` | `/admin/tenants/{id}/webhooks` | Register webhook |
| `POST` | `/admin/tenants/{id}/signing-key` | Register tenant signing key override |
| `POST` | `/admin/tenants/{id}/signing-key/rotate` | Rotate tenant signing key |
| `DELETE` | `/admin/tenants/{id}/signing-key` | Remove override; revert to global key |
| `GET` | `/admin/audit-log` | Query audit log |
| `POST` | `/admin/signing-keys/rotate` | Rotate global platform signing key |
| `GET` | `/.well-known/jwks.json` | All active public keys (global + tenant overrides) |

---

## Testing the API

```bash
# Validate a license
curl -X POST http://localhost:8080/licenses/validate \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: example_tenant_1" \
  -H "X-API-Key: your_api_key" \
  -d '{ "key": "ABC-123", "product": "v-plugin" }'

# Activate a seat
curl -X POST http://localhost:8080/licenses/activate \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: example_tenant_1" \
  -H "X-API-Key: your_api_key" \
  -d '{ "key": "ABC-123", "product": "v-plugin", "machine_id": "abc", "hostname": "ws-1" }'

# Download a signed license file
curl http://localhost:8080/licenses/ABC-123/signed \
  -H "X-Tenant-ID: example_tenant_1" \
  -H "X-API-Key: your_api_key"
```

---

## Versioning

This project follows semantic versioning (SemVer):

- **MAJOR**: Breaking changes.
- **MINOR**: New features and non-breaking enhancements.
- **PATCH**: Bug fixes and minor maintenance.

---

## Future Enhancements

- Distributed queue integration (Kafka / RabbitMQ) for webhook delivery at scale.
- Multi-region cache replication.
- Client SDKs: Node.js, Python, PHP (generated from OpenAPI spec).
- License analytics dashboard.

---

## Changelog

Please see [CHANGELOG.md](CHANGELOG.md) for a full release history.

---

## April 2026 updates: Schema, API, CLI, SDK

- Tenants: added name, slug (unique), email, company, plan (default: free), max_licenses (default: 1000), metadata JSONB, updated_at, deleted_at
- Licenses: added issued_at, revoked_at, revoked_reason, last_validated_at, version, deleted_at
- Activations: added ip, user_agent, metadata JSONB
- Usage: added source, metadata JSONB; new table usage_daily for rollups
- Audit: added resource_type, severity
- Webhooks: added last_triggered_at, failure_count; Deliveries: added error
- Products: added max_activations, usage_limit, trial_days
- Indexes: license status/expiry; usage license/time; activation active by tenant

API changes:
- Validation now returns meta.version (int) for client cache validation.
- New admin endpoint: PATCH /admin/tenants/:id/profile with body { name, slug, email, company, plan, max_licenses, metadata }.
- Admin revoke accepts { tenant_id, key, reason? }. Reason may be ignored depending on deployment.

CLI:
- New admin helpers via migrate tool:
  - Update tenant profile:
    ```bash
    go run ./cmd/migrate admin tenant update-profile \
      --id TENANT_ID \
      --name "Acme Inc" \
      --slug acme \
      --email ops@acme.io \
      --company "Acme Inc" \
      --plan pro \
      --max-licenses 5000 \
      --metadata '{"tier":"enterprise"}'
    ```
  - Revoke a license:
    ```bash
    go run ./cmd/migrate admin license revoke \
      --tenant TENANT_ID \
      --key LICENSE_KEY
    ```
- Existing Cobra CLI (cmd/cli) continues to manage tenants and licenses; no breaking changes.

SDK guidance:
- Include new optional fields in models:
  - Tenants: plan, max_licenses, metadata, updated_at, deleted_at
  - Licenses: issued_at, revoked_at, revoked_reason, last_validated_at, version, deleted_at
  - Activations: ip, user_agent, metadata
- Ensure validation response model includes meta.version.

Cache/sync:
- Admin updates write-through caches; tenant.updated_at maintained by trigger.
- License version auto-bumps on meaningful DB updates; validation remains cache-only.

---

## Maintainer

**Ravi K Gupta**

- **Website**: [devravik.github.io](https://devravik.github.io/)
- **Email**: `dev.ravikgupt@gmail.com`
- **LinkedIn**: [linkedin.com/in/ravi-k-dev](https://www.linkedin.com/in/ravi-k-dev)
- **GitHub**: [github.com/devravik](https://github.com/devravik)

---

## License

The MIT License (MIT). Please see [LICENSE](LICENSE) for more information.