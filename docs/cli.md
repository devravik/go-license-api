# CLI Reference

The project ships a Cobra-based CLI that covers all control-plane operations. It talks directly to the running server via the admin API — no direct database access.

**Build once:**

```bash
go build -o golicense ./cmd/cli
```

Or run without building:

```bash
go run ./cmd/cli <command>
```

Add `--pretty` to any command to get formatted JSON output.

---

## Tenant

```bash
# Create a tenant. Returns tenant ID and API key.
golicense tenant create --rps=100 --burst=200 --pretty

# List all tenants
golicense tenant list

# Get a single tenant
golicense tenant get --id=tenant_1

# Update rate limits
golicense tenant update --id=tenant_1 --rps=200 --burst=400

# Rotate the tenant's API key (old key stays valid for the grace window)
golicense tenant rotate-key --id=tenant_1 --grace=24h

# Set IP allowlist (comma-separated CIDRs; replaces existing list)
golicense tenant allowlist --id=tenant_1 --cidr=10.0.0.0/8 --cidr=192.168.0.0/16

# Suspend a tenant (all their validation requests will be rejected immediately)
golicense tenant suspend --id=tenant_1 --reason="abuse"

# Reinstate a suspended tenant
golicense tenant reinstate --id=tenant_1

# Delete a tenant (triggers GDPR erasure job)
golicense tenant delete --id=tenant_1
```

---

## License

```bash
# Create a license
golicense license create \
  --tenant=tenant_1 \
  --key=ABC-123 \
  --product=v-plugin \
  --expires=2026-12-31 \
  --meta='{"plan":"pro"}'

# List licenses for a tenant
golicense license list --tenant=tenant_1 --limit=100 --offset=0

# Get a single license
golicense license get --tenant=tenant_1 --key=ABC-123

# Update a license (e.g. extend expiry)
golicense license update --tenant=tenant_1 --key=ABC-123 --expires=2027-01-01

# Revoke a license
golicense license revoke --tenant=tenant_1 --key=ABC-123
```

---

## Product

```bash
# Create a product
golicense product create \
  --tenant=tenant_1 \
  --id=v-plugin \
  --name="Video Plugin" \
  --version=1.0

# Update a product
golicense product update --tenant=tenant_1 --id=v-plugin --name="Video Plugin Pro"

# Get a product
golicense product get --tenant=tenant_1 --code=v-plugin

# List products for a tenant
golicense product list --tenant=tenant_1

# Activate / deactivate a product
golicense product activate --tenant=tenant_1 --id=<product_id>
golicense product deactivate --tenant=tenant_1 --id=<product_id>

# Delete a product
golicense product delete --tenant=tenant_1 --id=<product_id>
```

---

## Cache

```bash
# Show current cache stats (hit rate, size, evictions)
golicense cache stats

# Invalidate all cache entries for a tenant
golicense cache invalidate --tenant=tenant_1

# Warm up the cache from the database (control-plane only)
golicense cache warmup --limit=500

# Reload cache entries for a tenant (invalidate + warm up)
golicense cache reload --tenant=tenant_1
golicense cache reload --limit=500
```

---

## System

```bash
# Check server health
golicense system health

# Show active configuration (resolved env values)
golicense system config

# Show runtime stats (workers, queue depth, cache size)
golicense system stats
```

---

## Notes

- All CLI commands require the server to be running and `ADMIN_API_KEY` to be set in the environment (or passed via `--api-key`).
- The CLI is stateless — it makes HTTP calls to the admin API, it does not connect to the database directly.
- For deeper per-command output and flag documentation, pass `--help` to any command.
