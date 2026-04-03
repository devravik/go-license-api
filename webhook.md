# Webhooks Guide

This guide explains how to register, secure, and consume webhooks emitted by Go License API.

---

## Overview

The service emits signed HTTP POST events asynchronously for license lifecycle changes. Delivery is decoupled from request handling and retried with backoff.

Typical uses:
- Update internal state after `license.revoked`
- Notify customers on `license.expired`
- Track activations and usage

---

## Registering a Webhook

Admin-only endpoint:

```
POST /admin/tenants/:id/webhooks
```

Request:

```json
{
  "url": "https://your-app.com/hooks/license",
  "events": ["license.validated", "license.revoked"],
  "secret": "your-signing-secret"
}
```

Response:

```json
{ "ok": true }
```

Required headers:
- `X-Admin-Key: <ADMIN_API_KEY>`

---

## Security Model

Defense-in-depth is enforced at registration and dispatch time:

- HTTPS-only: non-HTTPS URLs are rejected
- Private/loopback/link-local IPs are blocked
  - 127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16
  - IPv6 loopback/ULA: ::1/128, fc00::/7
- DNS rebinding protection: destination IP is resolved and rechecked at connect time
- Redirects disabled: 3xx responses are not followed
- Timeouts enforced: ~10s overall client timeout

If violated:
- Registration responds `400 {"error":"invalid_webhook_url","detail":"..."}`.
- Dispatch skips delivery and logs a notice.

---

## Delivery Details

- Method: `POST`
- Headers:
  - `Content-Type: application/json`
  - `User-Agent: GoLicenseAPI-Webhooks/1.0`
  - `X-License-Timestamp: <unix-seconds>`
  - `X-License-Signature: v1=<hex-hmac-sha256>`
  - `X-Webhook-Version: v1`
  - `X-Webhook-Id: <event-id>`
  - `X-Webhook-Attempt: <1..5>`
  - `X-Body-SHA256: <hex>`
- Retries: exponential backoff up to 5 attempts
- Redirects: not followed
- Max payload size: 256KB (deliveries exceeding this are dropped and logged)

Payload shape:

```json
{
  "id": "evt_abc123",
  "event": "license.revoked",
  "version": "v1",
  "tenant_id": "tenant_1",
  "occurred_at": "2026-04-03T12:00:00Z",
  "data": {
    "license_key": "LIC-ABC-123",
    "reason": "fraud"
  }
}
```

---

## Verifying Signatures

Compute HMAC-SHA256 over:

```
<timestamp> . <raw-json-body>
```

Using your registered secret. Compare the lowercase hex digest to `X-License-Signature` with constant-time compare.

Example (Node.js):

```javascript
const crypto = require('crypto');

function verifySignature(req, secret) {
  const ts = req.header('X-License-Timestamp');
  const sig = req.header('X-License-Signature');
  const body = req.rawBody; // raw string, not parsed JSON
  const msg = `${ts}.${body}`;
  const h = crypto.createHmac('sha256', secret).update(msg).digest('hex');
  const a = Buffer.from(h, 'hex');
  const b = Buffer.from(sig, 'hex');
  return a.length === b.length && crypto.timingSafeEqual(a, b);
}
```

Example (Go):

```go
func verifySignature(ts string, body []byte, secret []byte, got string) bool {
	msg := append(append([]byte(ts), '.'), body...)
	mac := hmac.New(sha256.New, secret)
	mac.Write(msg)
	sum := mac.Sum(nil)
	want, err := hex.DecodeString(got)
	if err != nil || len(want) != len(sum) {
	 return false
	}
	return subtle.ConstantTimeCompare(sum, want) == 1
}
```

Best practices:
- Reject if timestamp is too old (e.g., >5 minutes) to mitigate replay
- Use raw body bytes; avoid double-encoding
- Validate `X-Body-SHA256` matches your computed body hash
- Read event id from `X-Webhook-Id` for de-duplication/tracing
- Log `X-Webhook-Attempt` to understand retry behavior

---

## Retry Strategy

Explicit backoff schedule (attempt → delay):

1 → 1s, 2 → 5s, 3 → 25s, 4 → 2m, 5 → 10m

Non-2xx responses and network errors advance the attempt counter. No automatic redirects are followed.

---

## Ordering Semantics

Webhooks are not guaranteed to be strictly ordered. Clients must not assume sequence delivery; use `occurred_at` plus your own event store ordering if needed.

---

## Consuming Events Safely

- Respond with `2xx` on success; `>=300` triggers retry
- Idempotency: de-duplicate by `payload.id`
- Enforce short processing timeouts on your side
- Avoid making internal network calls based on untrusted data

---

## Troubleshooting

- Receiving nothing?
  - Confirm your URL is HTTPS and publicly reachable
  - Ensure IP allowlists or firewalls allow inbound traffic
- Signature mismatch?
  - Use the exact raw body bytes
  - Ensure secret matches the registered one
- Repeated retries?
  - Return a `2xx` after successfully handling; any other code will be retried

---

## Event Catalog (current)

- `license.validated`
- `license.validation_failed`
- `license.expired`
- `license.grace_period_started`
- `license.activated`
- `license.deactivated`
- `license.revoked`
- `quota.exceeded`
- `tenant.suspended`

