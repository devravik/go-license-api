# API Reference

All endpoints return JSON. The OpenAPI spec is at [openapi.yaml](openapi.yaml) and served at runtime via `GET /docs/openapi.yaml`.

---

## Authentication

**Data plane** (`/licenses/*`): Public and cache-first.

Requests MUST NOT include:

```
X-Admin-Key
```

`X-Admin-Key` is rejected with HTTP 400 `privileged_headers_not_allowed`.

**Control plane** (`/admin/*`): Pass the admin key in a header.

```
X-Admin-Key: your_admin_api_key
```

Admin endpoints also enforce IP CIDR restrictions if `ADMIN_ALLOWED_CIDRS` is set.

---

## Data plane
Public endpoints for validation and usage. Rate limiting:
- Per-license key bucket (primary)
- Per-IP fallback bucket if key not parsable
- Path-level global bucket for burst protection

All operations are cache-only: no database queries are performed in validation.

### Validate a license (plan-first resolved features)

`POST /licenses/validate`

```json
{
  "key": "LIC-ABC-123"
}
```

Optional:

```json
{
  "key": "LIC-ABC-123",
  "product_id": "prod_xxxxxxxx"
}
```

Success:

```json
{
  "valid": true,
  "meta": {
    "license_id": "lic_xxxxx",
    "status": "active",
    "type": "plan",
    "plan_id": "plan_xxxxxxxx",
    "plan": {
      "id": "plan_xxxxxxxx",
      "name": "Pro"
    },
    "product": {
      "id": "prod_xxxxxxxx",
      "name": "V Plugin"
    },
    "product_id": "prod_xxxxxxxx",
    "expires_at": "2026-12-31T00:00:00Z",
    "seats_total": -1,
    "unlimited_seats": true,
    "seats_used": 2,
    "trial": true,
    "grace_period_ends_at": null,
    "features": ["sso", "audit-log", "beta-feature"]
  }
}
```

Failure:

```json
{
  "valid": false,
  "error": {
    "code": "license_expired",
    "message": "License expired"
  },
  "grace_period_ends_at": "2026-01-14T00:00:00Z"
}
```

Possible error values: `license_not_found`, `license_expired`, `license_revoked`, `license_in_grace_period`, `tenant_suspended`, `ip_not_allowed`.

---

### Activate a seat

`POST /licenses/activate`

```json
{
  "key": "ABC-123",
  "client_id": "sha256-of-hardware-fingerprint",
  "hostname": "workstation-42"
}
```

Response:

```json
{
  "activated": true,
  "activation_id": "act_abc123",
  "seats_remaining": 3,
  "unlimited_seats": false,
  "license": {
    "license_id": "lic_xxxxx",
    "status": "active",
    "type": "plan",
    "plan_id": "plan_xxxxxxxx",
    "plan": {
      "id": "plan_xxxxxxxx",
      "name": "Pro"
    },
    "product": {
      "id": "prod_xxxxxxxx",
      "name": "V Plugin"
    },
    "product_id": "prod_xxxxxxxx",
    "expires_at": "2026-12-31T00:00:00Z",
    "seats_total": 10,
    "unlimited_seats": false,
    "trial": false,
    "grace_period_ends_at": null,
    "features": ["sso", "audit-log"]
  }
}
```

Unlimited seats response:

```json
{
  "activated": true,
  "activation_id": "act_abc123",
  "seats_remaining": null,
  "unlimited_seats": true,
  "license": {
    "license_id": "lic_xxxxx",
    "status": "active",
    "type": "plan",
    "plan_id": "plan_xxxxxxxx",
    "product_id": "prod_xxxxxxxx",
    "seats_total": -1,
    "unlimited_seats": true,
    "features": ["sso", "audit-log"]
  }
}
```

Seat limit exceeded:

```json
{
  "activated": false,
  "error": {
    "code": "seats_limit_exceeded",
    "message": "Seat limit reached"
  }
}
```

---

### Release a seat

`POST /licenses/deactivate`

```json
{
  "key": "ABC-123",
  "activation_id": "act_abc123"
}
```

---

### Report consumption

`POST /licenses/usage`

```json
{
  "key": "ABC-123",
  "units": 10
}
```

---

### Get a signed offline license

`GET /licenses/{key}/signed`

Returns a signed JSON payload containing the license data and an Ed25519 signature. The client can cache this locally and verify it offline using the public key from `/.well-known/jwks.json`.

```json
{
  "license_id": "lic_xxx",
  "license_key": "ABC-123",
  "type": "plan",
  "tenant_id": "ten_xxx",
  "plan_id": "plan_xxx",
  "product_id": "prod_xxx",
  "status": "active",
  "expires_at": "2026-12-31T00:00:00Z",
  "seats_total": -1,
  "seats_used": 2,
  "features": ["sso", "audit-log", "beta-feature"],
  "issued_at": "2024-01-01T00:00:00Z",
  "kid": "ten_xxx_key_01",
  "issuer": "ten_xxx",
  "signature": "<base64-encoded-ed25519-signature>"
}
```

The `kid` field identifies which public key to use for verification. Look it up in the JWKS response.

---

### Public key set (JWKS)

`GET /.well-known/jwks.json`

Returns all active public keys — global and tenant-specific overrides — as a JSON Web Key Set. Used by clients to verify signed offline licenses.

```json
{
  "keys": [
    {
      "kid": "global_2024_01",
      "issuer": "global",
      "kty": "OKP",
      "crv": "Ed25519",
      "x": "<base64url-encoded-public-key>"
    }
  ]
}
```

---

## Control plane
Two separate control planes:

- Platform: `/admin/*` requires `X-Admin-Key`. Writes to PostgreSQL and updates cache immediately. Bypasses rate limiter and worker pool.
- Tenant: `/tenant/*` requires `X-API-Key` (cache-validated) and enforces per-tenant CIDR allowlist. Writes directly and updates cache. Bypasses rate limiter and worker pool.

System tenant bootstrap and fallback:

- On startup, if no tenants exist, the system creates one tenant with a generated `ten_*` ID and `metadata.is_system=true`.
- If `tenant_id` is omitted, the system resolves the tenant with `metadata.is_system=true`. If no such tenant exists, the request fails.
- Internal services and repositories still require explicit `tenant_id` and never apply implicit fallback.
- Protected system tenant operations: suspend/delete are rejected with `system_tenant_protected`.

### Tenants

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/tenants` | Create a tenant |
| `PATCH` | `/admin/tenants/{id}/profile` | Update tenant profile (name, slug, email, plan) |
| `POST` | `/admin/tenants/{id}/suspend` | Suspend a tenant |
| `POST` | `/admin/tenants/{id}/reinstate` | Reinstate a tenant |
| `DELETE` | `/admin/tenants/{id}` | Delete tenant (GDPR erasure) |
| `POST` | `/admin/tenants/{id}/rotate-key` | Rotate tenant API key |
| `POST` | `/admin/tenants/{id}/ip-allowlist` | Set tenant IP allowlist |
| `POST` | `/admin/tenants/{id}/webhooks` | Register a webhook |
| `GET` | `/admin/audit-log` | Query audit log |

### Licenses

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/licenses` | Create a license |
| `GET` | `/admin/tenants/{tenant_id}/licenses/{key}` | Get a license |
| `PATCH` | `/admin/tenants/{tenant_id}/licenses/{key}` | Update a license |
| `POST` | `/admin/tenants/{tenant_id}/licenses/{key}/revoke` | Revoke a license |

`POST /admin/licenses` supports a strict resolve-or-create contract:

- **Mode 1 (reference existing, recommended)**: provide `plan_id` (for `type=plan`) or `product_id` (for `type=product`)
- **Mode 2 (inline create, controlled)**: provide `plan`/`product` nested object and include `Idempotency-Key` header (**required**)
- **Mode 3 (mixed)**: rejected (`plan_id + plan` or `product_id + product`)

Inline create rules:

- no implicit dedup by `name`
- no silent updates/merges
- inline objects create new entities

Trial strictness:

- if `trial.enabled=true`, then `trial.features` must be non-empty and `trial.ends_at` must be present
- if `seats_total != -1` and `seats_used >= seats_total`, activation fails with `seats_limit_exceeded`

### Products

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/products` | Create a product |
| `PATCH` | `/admin/products/{id}` | Update product active state/details |
| `DELETE` | `/admin/tenants/{tenant_id}/products/{id}` | Soft-delete a product |
| `POST` | `/admin/tenants/{tenant_id}/products/{id}/restore` | Restore a soft-deleted product |

### Plans (plan-first)

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/plans` | Create a plan |
| `GET` | `/admin/plans?tenant_id=...` | List plans for tenant (`tenant_id` optional; falls back to system tenant) |
| `GET` | `/admin/plans/{id}?tenant_id=...` | Get plan (`tenant_id` optional; falls back to system tenant) |
| `PATCH` | `/admin/plans/{id}` | Update plan |
| `PATCH` | `/admin/tenants/{tenant_id}/plans/{id}/active` | Set plan active/inactive |
| `DELETE` | `/admin/tenants/{tenant_id}/plans/{id}` | Soft-delete a plan |
| `POST` | `/admin/tenants/{tenant_id}/plans/{id}/restore` | Restore a soft-deleted plan |

### Audit log

| Method | Path | Description |
|---|---|---|
| `GET` | `/admin/audit-log` | Query audit log |

Query params: `tenant_id`, `event`, `from`, `to`, `limit`.

### Tenant control plane

| Method | Path | Description | Auth |
|---|---|---|---|
| `POST` | `/tenant/licenses` | Create a license | `X-API-Key` |
| `GET` | `/tenant/licenses/{key}` | Get a license | `X-API-Key` |
| `PATCH` | `/tenant/licenses/{key}` | Update a license | `X-API-Key` |
| `POST` | `/tenant/licenses/{key}/revoke` | Revoke a license | `X-API-Key` |
| `POST` | `/tenant/products` | Upsert a product | `X-API-Key` |
| `GET` | `/tenant/products/{id}` | Get a product | `X-API-Key` |
| `PATCH` | `/tenant/products/{id}` | Update a product | `X-API-Key` |
| `DELETE` | `/tenant/products/{id}` | Delete a product | `X-API-Key` |

`X-API-Key` is validated via in-memory cache and must be active and IP-allowed.

---

## Data model notes (plan-first)

- IDs are prefixed NanoIDs (default 12 chars): `ten_`, `prod_`, `plan_`, `lic_`.
- `license.type` rules:
  - `plan` => `plan_id` required, `product_id` null, `features` empty
  - `product` => `product_id` required, `plan_id` null, `features` required, `trial` disabled
- Trial resolution:
  - if active and `trial.features` present => use it
  - when `trial.enabled=true`, `trial.features` must be non-empty and `trial.ends_at` must be present
- Product validation:
  - `product_id` is optional in validation request
  - if provided, it must match license product linkage (`license.product_id` or plan-linked product)
- Override rule:
  - full `features` override cannot be combined with `features_add/features_remove`
- Feature resolution order:
  - base (`plan.features` or `license.features`)
  - trial replacement (if active)
  - override application (full override OR add/remove)
  - final resolved features are used for validation and signed-license payloads
- Seats:
  - `seats_total = -1` means unlimited
- Status lifecycle:
  - `active` -> `expired` (time-based)
  - `active` -> `revoked` (manual)
  - `expired` -> `revoked` (allowed)
  - `revoked` is terminal
- Activation uniqueness:
  - one active activation per `(license_key, client_id)`
  - same `client_id` returns existing `activation_id` (idempotent behavior)
- Soft delete behavior:
  - `products`, `plans`, and `licenses` use `deleted_at` soft-delete semantics
  - control-plane list/get/find operations exclude rows where `deleted_at IS NOT NULL`
  - delete operations set `deleted_at` instead of hard-delete
  - restore operations clear `deleted_at` and rehydrate cache immediately

---

## System

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Liveness — process is alive |
| `GET` | `/readyz` | Readiness — DB connected, cache ready |
| `GET` | `/docs/openapi.yaml` | OpenAPI 3.1 spec |
| `GET` | `/docs` | Swagger UI (development mode only) |
