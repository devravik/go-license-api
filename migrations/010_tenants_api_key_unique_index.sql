-- Enforce API key uniqueness for integrity and deterministic auth lookups.
-- Fails with a clear message if duplicates already exist.

DO $$
BEGIN
	IF EXISTS (
		SELECT 1
		FROM tenants
		WHERE api_key IS NOT NULL
		GROUP BY api_key
		HAVING COUNT(*) > 1
	) THEN
		RAISE EXCEPTION 'cannot create unique index on tenants(api_key): duplicate api_key values exist';
	END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_api_key_unique
ON tenants (api_key);

-- Old non-unique index becomes redundant after unique index exists.
DROP INDEX IF EXISTS idx_tenant_api_key;
