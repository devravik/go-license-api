# API Reference

All endpoints return JSON. The OpenAPI spec is at [openapi.yaml](openapi.yaml) and served at runtime via `GET /docs/openapi.yaml`.

---

## Authentication

**Data plane** (`/licenses/*`): Pass tenant credentials in headers.

```
X-Tenant-ID: tenant_1
X-API-Key: your_tenant_api_key
```

In `APP_MODE=single`, these headers are not required — a default tenant is used.

**Control plane** (`/admin/*`): Pass the admin key in a header.

```
X-Admin-Key: your_admin_api_key
```

Admin endpoints also enforce IP CIDR restrictions if `ADMIN_ALLOWED_CIDRS` is set.

---

## Data plane

### Validate a license

`POST /licenses/validate`

```json
{
  "key": "ABC-123",
  "product": "v-plugin"
}
```

Success:

```json
{
  "valid": true,
  "meta": {
    "plan": "pro",
    "expires_at": "2026-12-31",
    "seats_total": 10,
    "seats_used": 2,
    "trial": false,
    "grace_period_ends_at": null,
    "features": ["sso", "audit-log"]
  }
}
```

Failure:

```json
{
  "valid": false,
  "error": "license_expired",
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
  "license_key": "ABC-123",
  "product": "v-plugin",
  "tenant_id": "tenant_1",
  "plan": "pro",
  "expires_at": "2026-12-31T00:00:00Z",
  "seats": 10,
  "features": ["sso", "audit-log"],
  "issued_at": "2024-01-01T00:00:00Z",
  "kid": "tenant_1_key_01",
  "issuer": "tenant_1",
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

All `/admin/*` endpoints require `X-Admin-Key`. They write to PostgreSQL and update the cache immediately. They bypass the worker pool and rate limiter.

### Tenants

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/tenants` | Create a tenant |
| `GET` | `/admin/tenants` | List all tenants |
| `GET` | `/admin/tenants/{id}` | Get a tenant |
| `PATCH` | `/admin/tenants/{id}/profile` | Update tenant profile (name, slug, email, plan) |
| `POST` | `/admin/tenants/{id}/suspend` | Suspend a tenant |
| `POST` | `/admin/tenants/{id}/reinstate` | Reinstate a tenant |
| `DELETE` | `/admin/tenants/{id}` | Delete tenant (GDPR erasure) |
| `POST` | `/admin/tenants/{id}/rotate-key` | Rotate tenant API key |
| `POST` | `/admin/tenants/{id}/ip-allowlist` | Set tenant IP allowlist |
| `POST` | `/admin/tenants/{id}/webhooks` | Register a webhook |
| `GET` | `/admin/tenants/{id}/webhooks/{webhook_id}/deliveries` | Query webhook delivery history |
| `POST` | `/admin/tenants/{id}/signing-key` | Register a tenant signing key override |
| `POST` | `/admin/tenants/{id}/signing-key/rotate` | Rotate tenant signing key |
| `DELETE` | `/admin/tenants/{id}/signing-key` | Remove tenant signing key (reverts to global) |

### Licenses

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/licenses` | Create a license |
| `GET` | `/admin/licenses` | List licenses (filterable by tenant) |
| `GET` | `/admin/licenses/{key}` | Get a license |
| `PATCH` | `/admin/licenses/{key}` | Update a license |
| `POST` | `/admin/licenses/{key}/revoke` | Revoke a license |

### Products

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/products` | Create a product |
| `GET` | `/admin/products` | List products (filterable by tenant) |
| `GET` | `/admin/products/{id}` | Get a product |
| `PATCH` | `/admin/products/{id}` | Update a product |
| `DELETE` | `/admin/products/{id}` | Delete a product |

### Signing keys

| Method | Path | Description |
|---|---|---|
| `POST` | `/admin/signing-keys/rotate` | Rotate the global platform signing key |

### Audit log

| Method | Path | Description |
|---|---|---|
| `GET` | `/admin/audit-log` | Query audit log |

Query params: `tenant_id`, `event`, `from`, `to`, `limit`.

---

## System

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Liveness — process is alive |
| `GET` | `/readyz` | Readiness — DB connected, cache ready |
| `GET` | `/docs/openapi.yaml` | OpenAPI 3.1 spec |
| `GET` | `/docs` | Swagger UI (development mode only) |
