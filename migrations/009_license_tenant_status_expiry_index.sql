-- Optional composite index for tenant+status+expiry filters.

CREATE INDEX IF NOT EXISTS idx_license_tenant_status_expiry
ON licenses (tenant_id, status, expires_at);
