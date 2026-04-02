-- Products: Optional product-based licensing metadata (control plane).
-- Designed for full in-memory caching: small rows, tenant-scoped, fast lookup by (tenant_id, code).

CREATE TABLE products (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,

    code TEXT NOT NULL,              -- stable identifier used in API
    name TEXT NOT NULL,

    version TEXT,                    -- optional (v1, v2, etc.)
    is_active BOOLEAN DEFAULT TRUE,

    features JSONB DEFAULT '[]',     -- feature flags (JSON array of strings)
    meta JSONB DEFAULT '{}',         -- extensibility

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes
CREATE INDEX idx_products_tenant ON products(tenant_id);
CREATE UNIQUE INDEX uq_products_tenant_code ON products(tenant_id, code);

-- Keep updated_at current on any UPDATE (for efficient cache sync).
CREATE OR REPLACE FUNCTION set_updated_at_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER trg_products_set_updated_at
BEFORE UPDATE ON products
FOR EACH ROW
EXECUTE PROCEDURE set_updated_at_timestamp();

