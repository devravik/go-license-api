# Go License API Reference

This document explains all currently exposed HTTP APIs in this service, including:

- endpoint purpose
- authentication requirements
- request attributes
- example request/response
- common error responses

Base URL examples in this document use:

- `http://localhost:3000` (local)
- replace with your production domain for real usage

## Authentication Model

### Tenant APIs (`/licenses/*`)

Required headers:

- `X-API-Key: <tenant-api-key>`

### Admin APIs (`/admin/*`)

Required header:

- `X-Admin-Key: <admin-api-key>`

Optional network restriction:

- requests may also be blocked by admin CIDR allowlist if configured

---

## Public APIs

### 1) Home

- **Method:** `GET`
- **Path:** `/`
- **Auth:** none

Response:

```json
{
  "message": "Go License API",
  "status": "ok"
}
```

---

### 2) Health

- **Method:** `GET`
- **Path:** `/health`
- **Auth:** none

Response:

```json
{
  "status": "up"
}
```

Also available:

- `GET /healthz` (same response)

---

### 3) Readiness

- **Method:** `GET`
- **Path:** `/readyz`
- **Auth:** none

Ready response:

```json
{
  "status": "ready",
  "queue_depth": 0
}
```

Not-ready response:

```json
{
  "status": "starting"
}
```

---

### 4) OpenAPI + Docs

- `GET /openapi.yaml`
- `GET /openapi.json`
- `GET /docs` (Swagger UI)
- `GET /redoc` (ReDoc UI)

Auth: none

---

### 5) JWKS (if signing is enabled)

- **Method:** `GET`
- **Path:** `/.well-known/jwks.json`
- **Auth:** none

Response shape:

```json
{
  "keys": [
    {
      "kid": "global-1",
      "issuer": "Go License API",
      "kty": "OKP",
      "crv": "Ed25519",
      "x": "base64url-public-key",
      "alg": "EdDSA"
    }
  ]
}
```

---

## Tenant APIs (`/licenses`)

Common required headers for all endpoints in this section:

```http
X-API-Key: <tenant-api-key>
Content-Type: application/json
```

### 1) Validate License

- **Method:** `POST`
- **Path:** `/licenses/validate`

Request attributes:

- `license_key` (string, required): license key
- `client_id` (string, optional): normalized identifier where license is used (e.g., `example.com` or device id)
- `product_code` (string, optional): expected product code to match

Example request:

```json
{
  "license_key": "LIC-ABC-123",
  "client_id": "example.com",
  "product_code": "desktop-pro"
}
```

Success response:

```json
{
  "success": true,
  "valid": true,
  "license": {
    "plan": "pro",
    "product": "desktop-pro",
    "expires_at": "2026-12-31T23:59:59Z",
    "seats_total": 10,
    "trial": false,
    "grace_period_ends_at": "2027-01-07T23:59:59Z",
    "features": ["offline", "priority_support"],
    "version": 3,
    "in_grace_period": false
  },
  "request_id": "req-1736040000000000000",
  "timestamp": "2026-04-03T12:00:00Z"
}
```

Invalid response example:

```json
{
  "success": false,
  "valid": false,
  "error": {
    "code": "license_not_found",
    "message": "License not found"
  },
  "request_id": "req-1736040000000000001",
  "timestamp": "2026-04-03T12:00:00Z"
}
```

Possible errors:

- `invalid_request_body`
- `key_is_required`
- `validation_timeout`
- `invalid_key`
- `invalid_tenant`
- `tenant_suspended`
- `license_not_found`
- `license_revoked`
- `license_expired`
- `invalid_product`

---

### 2) Activate License

- **Method:** `POST`
- **Path:** `/licenses/activate`

Request attributes:

- `license_key` (string, required): license key
- `client_id` (string, required): stable identifier where license is used
- `hostname` (string, optional): diagnostics metadata only

Example request:

```json
{
  "license_key": "LIC-ABC-123",
  "client_id": "example.com",
  "hostname": "dev-box-01"
}
```

Success response:

```json
{
  "success": true,
  "activated": true,
  "client_id": "example.com",
  "seats": {
    "used": 6,
    "total": 10,
    "remaining": 4
  },
  "request_id": "req-1736040000000000100",
  "timestamp": "2026-04-03T12:00:01Z"
}
```

Error response example:

```json
{
  "success": false,
  "activated": false,
  "error": {
    "code": "seat_limit_reached",
    "message": "Seat limit reached"
  },
  "request_id": "req-1736040000000000101",
  "timestamp": "2026-04-03T12:00:01Z"
}
```

Possible errors:

- `activation_not_enabled`
- `invalid_request`
- `key_and_client_id_required`
- `seat_limit_reached`
- `license_not_found`
- `license_expired`
- `license_revoked`
- `license_in_grace_period`
- `internal_error`

Idempotency support:

- Optional request header: `Idempotency-Key`
- Same tenant + idempotency key can return cached activation response

---

### 3) Deactivate License

- **Method:** `POST`
- **Path:** `/licenses/deactivate`

Request attributes:

- `license_key` (string, required): license key
- `client_id` (string, required): stable identifier where license is used

Example request:

```json
{
  "license_key": "LIC-ABC-123",
  "client_id": "example.com"
}
```

Success response:

```json
{
  "success": true,
  "deactivated": true,
  "request_id": "req-1736040000000000200",
  "timestamp": "2026-04-03T12:00:02Z"
}
```

Error response example:

```json
{
  "success": false,
  "deactivated": false,
  "error": {
    "code": "license_not_found",
    "message": "License not found"
  },
  "request_id": "req-1736040000000000201",
  "timestamp": "2026-04-03T12:00:02Z"
}
```

Possible errors:

- `activation_not_enabled`
- `invalid_request`
- `key_and_client_id_required`
- `license_not_found`
- `internal_error`

---

### 4) Record Usage

- **Method:** `POST`
- **Path:** `/licenses/usage`

Request attributes:

- `license_key` (string, required): license key
- `units` (integer, required): usage units to consume (`1..1000000`)
- `event_id` (string, optional but recommended): client-side unique event id for de-duplication

Example request:

```json
{
  "license_key": "LIC-ABC-123",
  "units": 25,
  "event_id": "usage-123"
}
```

Success response:

```json
{
  "success": true,
  "recorded": true,
  "usage": {
    "total_used": 120,
    "remaining": 880
  },
  "request_id": "req-1736040000000000300",
  "timestamp": "2026-04-03T12:00:03Z"
}
```

Error response example:

```json
{
  "success": false,
  "recorded": false,
  "error": {
    "code": "invalid_units",
    "message": "Invalid usage units"
  },
  "request_id": "req-1736040000000000301",
  "timestamp": "2026-04-03T12:00:03Z"
}
```

Possible errors:

- `usage_not_enabled`
- `invalid_request`
- `key_required`
- `invalid_key`
- `invalid_units`
- `license_not_found`
- `license_revoked`
- `license_expired`
- `internal_error`

---

### 5) Get Signed License (if signing enabled)

- Methods:
  - `GET /licenses/:license_key/signed` (preferred)
  - `GET /licenses/:key/signed` (legacy)
  - `POST /licenses/signed` (preferred)
- Auth: tenant header required

Path/body parameter:

- `license_key` (string, required): license key

Success response shape:

```json
{
  "payload": "{\"license_key\":\"LIC-ABC-123\",\"product\":\"desktop-pro\",\"tenant_id\":\"...\",\"plan\":\"pro\",\"expires_at\":\"...\",\"not_before\":\"...\",\"issued_at\":\"...\",\"seats\":10,\"features\":[\"offline\"],\"kid\":\"global-1\",\"issuer\":\"Go License API\"}",
  "signature": "base64-signature",
  "kid": "global-1",
  "issuer": "Go License API"
}
```

Possible errors:

- `license_not_found`
- `signing_failed`

---

## Admin APIs (`/admin`)

Common required header:

```http
X-Admin-Key: <admin-api-key>
Content-Type: application/json
```

### 1) Admin Status

- **Method:** `GET`
- **Path:** `/admin/`

Response:

```json
{
  "message": "Admin API - Operational"
}
```

---

### 2) Create Tenant

- **Method:** `POST`
- **Path:** `/admin/tenants`

Request attributes:

- `rps` (integer, optional, default `100`)
- `burst` (integer, optional, default `200`)

Example request:

```json
{
  "rps": 150,
  "burst": 300
}
```

Success response:

```json
{
  "tenant_id": "9f8f9d4b-904e-4e18-b8b3-2ef6723f6112",
  "api_key": "generated_api_key",
  "limits": {
    "rps": 150,
    "burst": 300
  }
}
```

---

### 3) Revoke License

- **Method:** `POST`
- **Path:** `/admin/licenses/revoke`

Request attributes:

- `tenant_id` (string, required)
- `license_key` (string, required)
- `reason` (string, optional)

Example request:

```json
{
  "tenant_id": "9f8f9d4b-904e-4e18-b8b3-2ef6723f6112",
  "license_key": "LIC-ABC-123",
  "reason": "fraud"
}
```

Success response:

```json
{
  "ok": true
}
```

---

### 4) Suspend Tenant

- **Method:** `POST`
- **Path:** `/admin/tenants/:id/suspend`

Path attribute:

- `id` (string, required): tenant id

Request attributes:

- `reason` (string, optional)

Success response:

```json
{
  "ok": true
}
```

---

### 5) Reinstate Tenant

- **Method:** `POST`
- **Path:** `/admin/tenants/:id/reinstate`

Success response:

```json
{
  "status": "active"
}
```

---

### 6) Update Tenant IP Allowlist

- **Method:** `POST`
- **Path:** `/admin/tenants/:id/ip-allowlist`

Request attributes:

- `cidrs` (array of string, required): CIDR blocks

Example request:

```json
{
  "cidrs": ["203.0.113.0/24", "198.51.100.10/32"]
}
```

Success response:

```json
{
  "status": "updated"
}
```

---

### 7) Update Tenant Profile

- **Method:** `PATCH`
- **Path:** `/admin/tenants/:id/profile`

Request attributes:

- `name` (string)
- `slug` (string)
- `email` (string)
- `company` (string)
- `plan` (string)
- `max_licenses` (integer)
- `metadata` (object)

Example request:

```json
{
  "name": "Acme Inc",
  "slug": "acme",
  "email": "billing@acme.com",
  "company": "Acme Inc",
  "plan": "enterprise",
  "max_licenses": 500,
  "metadata": {
    "region": "ap-south-1"
  }
}
```

Success response:

```json
{
  "status": "updated"
}
```

---

### 8) Register Webhook

- **Method:** `POST`
- **Path:** `/admin/tenants/:id/webhooks`

Request attributes:

- `url` (string, required)
- `events` (array of string, required)
- `secret` (string, required)

Example request:

```json
{
  "url": "https://example.com/webhooks/license",
  "events": ["license.validated", "license.revoked"],
  "secret": "super-secret-signing-string"
}
```

Success response:

```json
{
  "ok": true
}
```

Security constraints on `url`:

- Must use `https` scheme
- Must resolve to public IPs only (no private/loopback/link-local, including: 127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, ::1/128, fc00::/7)
- Subject to DNS-rebinding protections at dispatch time (IP rechecked during connection)
- Redirects are not followed; responses >299 are treated as failures
- Delivery client enforces timeouts (default ~10s)

On violation, the server returns:

```json
{
  "error": "invalid_webhook_url",
  "detail": "only https scheme allowed for webhooks"
}
```

Delivery headers:

- `X-Webhook-Version: v1`
- `X-Webhook-Id: <event-id>`
- `X-Webhook-Attempt: <1..5>`
- `X-Body-SHA256: <hex>`
- `X-License-Timestamp: <unix-seconds>`
- `X-License-Signature: v1=<hex-hmac-sha256>`

Payload includes a `version` field, currently `"v1"`. Max payload size is 256KB; larger payloads are dropped and logged. Retries follow 1s → 5s → 25s → 2m → 10m; redirects are not followed.

---

### 9) Rotate Tenant API Key

- **Method:** `POST`
- **Path:** `/admin/tenants/:id/rotate-key`

Alias supported:

- `POST /admin/tenants/:id/rotate_key`

Request attributes:

- `grace_minutes` (integer, optional, default `60`, max `1440`)

Success response:

```json
{
  "new_api_key": "new-generated-api-key",
  "old_key_grace_expires_at": "2026-04-03T12:34:56Z"
}
```

---

### 10) Update Tenant Limits

- **Method:** `PATCH`
- **Path:** `/admin/tenants/:id/limits`

Request attributes:

- `rps` (integer, required)
- `burst` (integer, required)

Example request:

```json
{
  "rps": 200,
  "burst": 400
}
```

Success response:

```json
{
  "status": "updated"
}
```

---

### 11) Delete Tenant

- **Method:** `DELETE`
- **Path:** `/admin/tenants/:id`

Success response:

- HTTP `204 No Content`

---

### 12) Query Audit Log

- **Method:** `GET`
- **Path:** `/admin/audit-log`

Query parameters:

- `tenant_id` (string, optional)
- `event` (string, optional)
- `from` (RFC3339 timestamp, optional)
- `to` (RFC3339 timestamp, optional)
- `limit` (integer, optional, max 500, default 100)

Example request:

```bash
curl -H "X-Admin-Key: $ADMIN_API_KEY" \
  "http://localhost:3000/admin/audit-log?tenant_id=t-1&event=license.validated&limit=50"
```

Success response:

```json
{
  "entries": [
    {
      "id": "c2cc6f3e-35c5-4d28-a4ef-63b6df9b63d0",
      "tenant_id": "t-1",
      "actor_id": "system",
      "actor_ip": "0.0.0.0",
      "event": "license.validated",
      "resource_id": "LIC-ABC-123",
      "outcome": "success",
      "meta": {},
      "created_at": "2026-04-03T10:01:02Z"
    }
  ],
  "count": 1
}
```

---

## Common Auth Errors

Tenant auth failures:

- `401 {"success":false,"error":{"code":"missing_api_key","message":"Missing API key"}}`
- `401 {"success":false,"error":{"code":"invalid_api_key_format","message":"Invalid API key format"}}`
- `401 {"success":false,"error":{"code":"invalid_api_key","message":"Invalid API key"}}`
- `403 {"success":false,"error":{"code":"tenant_suspended","message":"Tenant suspended"}}`

Admin auth failures:

- `401 {"success":false,"error":{"code":"invalid_admin_key","message":"Invalid admin key"}}`
- `403 {"success":false,"error":{"code":"ip_not_allowed","message":"IP is not allowed"}}`

Rate limiting:

- `429 {"success":false,"error":{"code":"rate_limit_exceeded","message":"Rate limit exceeded"}}`

---

## cURL Quick Start

### Validate

```bash
curl -X POST http://localhost:3000/licenses/validate \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $TENANT_API_KEY" \
  -d '{"license_key":"LIC-ABC-123","client_id":"example.com","product_code":"desktop-pro"}'
```

### Activate

```bash
curl -X POST http://localhost:3000/licenses/activate \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $TENANT_API_KEY" \
  -H "Idempotency-Key: act-001" \
  -d '{"license_key":"LIC-ABC-123","client_id":"example.com","hostname":"dev-box-01"}'
```

### Signed License (preferred JSON)

```bash
curl -X POST http://localhost:3000/licenses/signed \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $TENANT_API_KEY" \
  -d '{"license_key":"LIC-ABC-123"}'
```

### Admin Create Tenant

```bash
curl -X POST http://localhost:3000/admin/tenants \
  -H "Content-Type: application/json" \
  -H "X-Admin-Key: $ADMIN_API_KEY" \
  -d '{"rps":100,"burst":200}'
```

---

## Notes

- Use `/openapi.json` for generated-tool integrations and strict schema checks.
- API error text is implementation-defined and may evolve; build clients to handle unknown error strings safely.
