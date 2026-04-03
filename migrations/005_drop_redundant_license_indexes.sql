-- Remove redundant license indexes.
-- UNIQUE (tenant_id, key) already covers tenant+key lookups and uniqueness.
-- Keeping duplicate indexes increases write overhead and memory usage.

DROP INDEX IF EXISTS idx_license_key;
DROP INDEX IF EXISTS idx_license_tenant_key;
