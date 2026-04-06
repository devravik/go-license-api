## Offline Signed Licenses

This document explains what Offline Signed Licenses are, why you might need them, how they work in this API, how to set them up, and how to use them in clients (including offline/air‑gapped environments).

### What it is

An Offline Signed License is a detached-signature JSON envelope that contains a canonical JSON payload of license claims and a signature (Ed25519). Clients can cache and verify this payload locally without contacting the server.

- Endpoint: `GET /licenses/{key}/signed`
- Public JWKS: `GET /.well-known/jwks.json`

The payload includes identity and constraints (e.g., `not_before`, `expires_at`, and optional soft-binding details). The server signs the payload so clients can verify authenticity locally.

### Why you need it

- **Offline/air‑gapped**: Let your app continue functioning without network access.
- **Resilience**: Survive temporary outages while maintaining license controls.
- **Performance**: Reduce online calls on every start in desktop/edge scenarios.

Trade-off: Offline payloads cannot alone enforce server-side constraints like seat limits. See Soft Binding to mitigate copying across devices.

### How it works

- **Signature scheme**: Ed25519 (EdDSA).
- **Verification keys**: Served via JWKS at `/.well-known/jwks.json`.
- **Signed envelope**:
  - `payload`: Canonical JSON string of claims.
  - `signature`: Base64-encoded Ed25519 signature over raw `payload` bytes.
  - `kid` and `issuer`: Key selection and provenance.

Key payload fields (subset):
- **identity/context**:
  - `license_id`, `license_key`, `type` (plan|product), `tenant_id`, optional `plan_id`, `product_id`
  - `status`
- **time bounds**:
  - `not_before`: license invalid before this time
  - `expires_at`: end of validity window
  - `issued_at`: issuance timestamp
- **features/limits**:
  - `features`, `seats_total`, `seats_used` (informational)
  - `max_offline_duration` (seconds): optional, client MUST revalidate online after this many seconds from the last successful online validation; 0 means no explicit limit from server.
- **soft binding (optional, recommended)**:
  - `activation_id`: the server-side activation this payload is bound to
  - `client_id`: device fingerprint identifier the payload is intended for
  - `binding_required`: when true, clients MUST reject if local fingerprint ≠ `client_id`
- **revocation**:
  - `revocation_id`: stable per-license id for offline revocation matching
- **replay**:
  - `jti`: per-issuance unique id; clients SHOULD attach to online calls for anomaly detection
- **versioning**:
  - `schema_version`: integer for forwards/backwards-compat handling

Client rules:
- MUST verify signature: `ed25519(public_key, payload_bytes, base64(signature))`
- MUST enforce `not_before` and `expires_at`
- SHOULD allow ±5 minutes clock skew
- If `max_offline_duration > 0`: MUST revalidate online after that many seconds
- If `client_id` present: SHOULD check it matches the local device fingerprint
- If `activation_id` present: SHOULD treat payload as bound to that activation and avoid reuse on other devices

### Setup

1) Configure signing
- Generate a signing key:

```bash
go run ./cmd/keygen --out .keys/signing_key.b64
```

- Set environment variable in `.env`:

```bash
SIGNING_KEY_PATH=.keys/signing_key.b64
```

2) Start the server
- Ensure `ADMIN_API_KEY` is set (admin endpoints) and database is reachable.
- Start the server normally. The server exposes:
  - `GET /licenses/{key}/signed`
  - `GET /.well-known/jwks.json`

3) Optional soft binding (activations)
- Use `POST /licenses/activate` with `client_id` to create an activation.
- When requesting a signed license, include the same `client_id`:

```bash
curl -s "https://api.example.com/licenses/LIC-ABC-123/signed?client_id=<sha256-hw-id>"
```

If a matching active activation exists, the signed payload will include `activation_id` and `client_id`.

4) Optional max offline window
- To set a revalidation window, populate license `metadata.max_offline_duration` (seconds) via admin flows; the signer will include it in the payload.

### Using it in clients

Example flow:
1. Fetch signed license envelope once online (or bundled by your installer).
2. Cache the envelope locally (e.g., in your app config directory).
3. On app start:
   - Load cached envelope
   - Parse `payload` as JSON; read `kid` from envelope to select public key from JWKS (cache keys and refresh occasionally)
   - Verify `signature` over raw `payload` bytes
   - Enforce time bounds and optional rules:
     - `now + skew >= not_before`
     - `now - skew <= expires_at`
     - If `max_offline_duration > 0`: ensure `now - lastSuccessfulOnlineValidation <= max_offline_duration`
     - If `client_id` present: ensure it matches your local fingerprint
4. If any rule fails, fall back to online validation (and/or block as your UX dictates).

Pseudo-code:

```pseudo
env = read_file("license_signed.json")
payload = parse_json(env.payload)    // Canonical JSON string
sig = base64_decode(env.signature)
pub = jwks_select(env.kid)
if !ed25519_verify(pub, bytes(env.payload), sig): deny("signature_invalid")

now = time_now()
skew = 5m
if payload.not_before and now + skew < parse_time(payload.not_before): deny("license_not_active")
if payload.expires_at  and now - skew > parse_time(payload.expires_at):  deny("license_expired")
if now + skew < parse_time(payload.issued_at): deny(\"clock_tampered\")

if payload.max_offline_duration > 0:
  if now - last_online_validation_at > payload.max_offline_duration:
    require_online_revalidate()

if payload.client_id and payload.client_id != local_fingerprint():
  deny("license_bound_to_different_device")

if payload.revocation_id in local_revocation_cache():
  deny("license_revoked_offline")

grant_access(features=payload.features, seats_total=payload.seats_total)
```

Notes:
- Keep JWKS locally cached; refresh on `kid` miss or at reasonable intervals.
- Always use canonical raw `payload` bytes for signature verification — do not re-serialize.

### Security notes and limitations

- Offline signed payloads prove authenticity, not real-time state. They do not alone enforce seat limits or revocations in real time.
- Mitigations:
  - Use `max_offline_duration` to ensure regular online revalidation.
  - Use soft-binding (`activation_id`, `client_id`) so copies are rejected on other devices.
  - Keep `expires_at` tight for high-risk licenses.
  - Consider periodic online validation even when within the offline window.

### Troubleshooting

- Signature verification fails:
  - Ensure you verify the exact raw `payload` bytes from the envelope
  - Confirm you selected the correct public key (`kid`) from JWKS
  - Check base64 decoding and Ed25519 verification implementation

- Payload rejected as “not active” or “expired” unexpectedly:
  - Allow ±5 minutes clock skew on client devices
  - Check device system time
  - Check for `binding_required=true` and ensure `client_id` matches your local fingerprint

- `client_id` mismatch:
  - Ensure your local fingerprinting is stable and matches what you provided during activation

- Activation missing in payload:
  - Include `?client_id=...` when calling `GET /licenses/{key}/signed`
  - Ensure an active activation exists for `(license_key, client_id)`

### Revocation list distribution

- Admin service can export a compact list:

```bash
curl -H \"X-Admin-Key: $ADMIN_API_KEY\" \\
  \"https://api.example.com/admin/revocations?since=2026-01-01T00:00:00Z&limit=1000\"
```

- Clients periodically fetch (when online), cache locally, and block licenses whose `revocation_id` appears.

---

See also:
- API reference in `docs/api.md` (Signed License section)
- OpenAPI schema in `docs/openapi.yaml` (SignedLicenseEnvelope, SignedLicensePayload)

