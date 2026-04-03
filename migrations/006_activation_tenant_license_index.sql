-- Support activation queries scoped by tenant + license.
-- Existing partial index idx_activation_license (license_id) WHERE is_active=TRUE
-- already covers active-by-license lookups; do not duplicate it.

CREATE INDEX IF NOT EXISTS idx_activation_tenant_license
ON activations (tenant_id, license_id);
