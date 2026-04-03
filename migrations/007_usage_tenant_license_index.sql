-- Improve tenant-scoped usage queries.

CREATE INDEX IF NOT EXISTS idx_usage_tenant_license
ON usage_records (tenant_id, license_id);
