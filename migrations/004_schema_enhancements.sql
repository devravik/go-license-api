-- 004_schema_enhancements.sql
-- Extend core tables with identity, lifecycle, analytics, cache-sync fields, and indexes.
-- Idempotent where possible to allow safe re-runs.

-- =========================
-- Tenants
-- =========================
ALTER TABLE IF EXISTS tenants
	ADD COLUMN IF NOT EXISTS name TEXT,
	ADD COLUMN IF NOT EXISTS slug TEXT UNIQUE,
	ADD COLUMN IF NOT EXISTS email TEXT,
	ADD COLUMN IF NOT EXISTS company TEXT,
	ADD COLUMN IF NOT EXISTS plan TEXT DEFAULT 'free',
	ADD COLUMN IF NOT EXISTS max_licenses INT DEFAULT 1000,
	ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}',
	ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP;

-- set_updated_at_timestamp() exists from 002_products.sql; recreate defensively if missing
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1 FROM pg_proc WHERE proname = 'set_updated_at_timestamp'
	) THEN
		CREATE OR REPLACE FUNCTION set_updated_at_timestamp()
		RETURNS TRIGGER AS $func$
		BEGIN
			NEW.updated_at = CURRENT_TIMESTAMP;
			RETURN NEW;
		END;
		$func$ language 'plpgsql';
	END IF;
END$$;

DROP TRIGGER IF EXISTS trg_tenants_set_updated_at ON tenants;
CREATE TRIGGER trg_tenants_set_updated_at
BEFORE UPDATE ON tenants
FOR EACH ROW
EXECUTE PROCEDURE set_updated_at_timestamp();

-- =========================
-- Licenses
-- =========================
ALTER TABLE IF EXISTS licenses
	ADD COLUMN IF NOT EXISTS issued_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMP,
	ADD COLUMN IF NOT EXISTS revoked_reason TEXT,
	ADD COLUMN IF NOT EXISTS last_validated_at TIMESTAMP,
	ADD COLUMN IF NOT EXISTS version INT DEFAULT 1,
	ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP;

-- Bump license version on meaningful updates (ignore analytics-only changes)
CREATE OR REPLACE FUNCTION bump_license_version()
RETURNS TRIGGER AS $$
BEGIN
	-- If row changed and change is not only last_validated_at, bump version
	IF (NEW IS DISTINCT FROM OLD)
		AND NOT (
			NEW.last_validated_at IS DISTINCT FROM OLD.last_validated_at
			AND (NEW.* IS NOT DISTINCT FROM (OLD.*)::licenses) -- fallback guard
		)
	THEN
		NEW.version := COALESCE(OLD.version, 1) + 1;
	END IF;
	RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_licenses_bump_version ON licenses;
CREATE TRIGGER trg_licenses_bump_version
BEFORE UPDATE ON licenses
FOR EACH ROW
EXECUTE PROCEDURE bump_license_version();

-- =========================
-- Activations
-- =========================
ALTER TABLE IF EXISTS activations
	ADD COLUMN IF NOT EXISTS ip TEXT,
	ADD COLUMN IF NOT EXISTS user_agent TEXT,
	ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';

-- =========================
-- Usage Records
-- =========================
ALTER TABLE IF EXISTS usage_records
	ADD COLUMN IF NOT EXISTS source TEXT,
	ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';

CREATE TABLE IF NOT EXISTS usage_daily (
	tenant_id TEXT,
	license_id INT,
	date DATE,
	units INT,
	PRIMARY KEY (tenant_id, license_id, date)
);

-- =========================
-- Audit Log
-- =========================
ALTER TABLE IF EXISTS audit_log
	ADD COLUMN IF NOT EXISTS resource_type TEXT,
	ADD COLUMN IF NOT EXISTS severity TEXT DEFAULT 'info';

-- =========================
-- Webhooks
-- =========================
ALTER TABLE IF EXISTS webhooks
	ADD COLUMN IF NOT EXISTS last_triggered_at TIMESTAMP,
	ADD COLUMN IF NOT EXISTS failure_count INT DEFAULT 0;

ALTER TABLE IF EXISTS webhook_deliveries
	ADD COLUMN IF NOT EXISTS error TEXT;

-- =========================
-- Tenant signing keys
-- =========================
ALTER TABLE IF EXISTS tenant_signing_keys
	ADD COLUMN IF NOT EXISTS algorithm TEXT DEFAULT 'ed25519';

-- =========================
-- Products
-- =========================
ALTER TABLE IF EXISTS products
	ADD COLUMN IF NOT EXISTS max_activations INT,
	ADD COLUMN IF NOT EXISTS usage_limit INT,
	ADD COLUMN IF NOT EXISTS trial_days INT;

-- =========================
-- Indexes
-- =========================
CREATE INDEX IF NOT EXISTS idx_license_status ON licenses(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_license_expiry ON licenses(expires_at);
CREATE INDEX IF NOT EXISTS idx_usage_license_time ON usage_records(license_id, recorded_at);
CREATE INDEX IF NOT EXISTS idx_activation_active ON activations(tenant_id) WHERE is_active = TRUE;

