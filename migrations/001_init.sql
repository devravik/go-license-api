-- =============================================================================
-- Initial schema — complete, production-ready.
-- =============================================================================

BEGIN;

-- =============================================================================
-- Functions
-- =============================================================================

-- Generic updated_at auto-bump (used by tenants + products).
CREATE OR REPLACE FUNCTION set_updated_at_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Bump license version on any meaningful update (ignore analytics-only touch).
CREATE OR REPLACE FUNCTION bump_license_version()
RETURNS TRIGGER AS $$
BEGIN
    IF (NEW IS DISTINCT FROM OLD)
        AND NOT (
            NEW.last_validated_at IS DISTINCT FROM OLD.last_validated_at
            AND (NEW.* IS NOT DISTINCT FROM (OLD.*)::licenses)
        )
    THEN
        NEW.version := COALESCE(OLD.version, 1) + 1;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- Tables
-- =============================================================================

-- Tenants: API consumers (SaaS customers / organisations).
-- api_key_hash stores SHA-256(raw_api_key). Raw keys are never persisted.
CREATE TABLE tenants (
    id                  TEXT        PRIMARY KEY,
    -- EC-04: only the hash of the API key is stored; plaintext is never persisted.
    api_key_hash        TEXT        NOT NULL,
    -- EC-08: dual-key rotation — old key hash stays valid during grace period.
    old_api_key_hash    TEXT,
    old_key_expires_at  TIMESTAMP,
    rps                 INT         DEFAULT 100,
    burst               INT         DEFAULT 200,
    status              TEXT        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active','suspended','deleted')),
    suspended_at        TIMESTAMP,
    suspension_reason   TEXT,
    ip_allowlist        TEXT[]      DEFAULT NULL,
    name                TEXT,
    slug                TEXT        UNIQUE,
    email               TEXT,
    company             TEXT,
    plan                TEXT        DEFAULT 'free',
    max_licenses        INT         DEFAULT 1000,
    metadata            JSONB       DEFAULT '{}',
    created_at          TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    deleted_at          TIMESTAMP
);

CREATE TRIGGER trg_tenants_set_updated_at
BEFORE UPDATE ON tenants
FOR EACH ROW EXECUTE PROCEDURE set_updated_at_timestamp();

-- Products: Optional product catalogue (must be created before licenses for FK).
CREATE TABLE products (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id),
    code            TEXT        NOT NULL,   -- stable identifier used in API
    name            TEXT        NOT NULL,
    version         TEXT,
    is_active       BOOLEAN     DEFAULT TRUE,
    features        JSONB       DEFAULT '[]',
    meta            JSONB       DEFAULT '{}',
    max_activations INT         CHECK (max_activations > 0),
    usage_limit     INT         CHECK (usage_limit > 0),
    trial_days      INT         CHECK (trial_days >= 0),
    created_at      TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP   DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER trg_products_set_updated_at
BEFORE UPDATE ON products
FOR EACH ROW EXECUTE PROCEDURE set_updated_at_timestamp();

-- Licenses: Issued to end-users by a tenant.
CREATE TABLE licenses (
    id                  SERIAL      PRIMARY KEY,
    tenant_id           TEXT        NOT NULL REFERENCES tenants(id),
    key                 TEXT        NOT NULL,
    -- product_id is an optional loose reference to products.id.
    -- No FK enforced: licenses may use a free-text product_id or the
    -- product TEXT column independently without a catalogue entry.
    product_id          TEXT,
    product             TEXT,
    status              TEXT        NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active','revoked','expired','suspended')),
    plan                TEXT,
    is_trial            BOOLEAN     DEFAULT FALSE,
    trial_ends_at       TIMESTAMP,
    expires_at          TIMESTAMP,
    grace_period_days   INT         DEFAULT 0  CHECK (grace_period_days >= 0),
    seat_count          INT         DEFAULT NULL CHECK (seat_count > 0),
    max_activations     INT         DEFAULT NULL CHECK (max_activations > 0),
    usage_limit         INT         DEFAULT NULL CHECK (usage_limit >= 0),
    usage_used          INT         DEFAULT 0   CHECK (usage_used >= 0),
    -- JSONB for features: consistent with products.features; flexible for flag objects.
    features            JSONB       DEFAULT '[]',
    meta                JSONB,
    issued_at           TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    revoked_at          TIMESTAMP,
    revoked_reason      TEXT,
    last_validated_at   TIMESTAMP,
    version             INT         DEFAULT 1,
    created_at          TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    deleted_at          TIMESTAMP,
    -- UNIQUE(tenant_id, key) creates an implicit B-tree index; no separate
    -- idx_license_lookup is needed — PostgreSQL always uses this index for
    -- WHERE tenant_id = $1 AND key = $2 lookups.
    UNIQUE (tenant_id, key)
);

CREATE TRIGGER trg_licenses_bump_version
BEFORE UPDATE ON licenses
FOR EACH ROW EXECUTE PROCEDURE bump_license_version();

-- Activations: Seat-based activation records.
-- client_id is the unified, normalised identifier for where a license is used.
-- UUID type ensures valid UUID storage and 16-byte efficiency.
CREATE TABLE activations (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    license_id      INT         NOT NULL REFERENCES licenses(id),
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id),
    client_id       TEXT        NOT NULL,
    hostname        TEXT,
    is_active       BOOLEAN     DEFAULT TRUE,
    activated_at    TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    released_at     TIMESTAMP,
    ip              TEXT,
    user_agent      TEXT,
    metadata        JSONB       DEFAULT '{}'
);

-- Usage records: Consumption-based metered usage.
CREATE TABLE usage_records (
    id          SERIAL      PRIMARY KEY,
    license_id  INT         NOT NULL REFERENCES licenses(id),
    tenant_id   TEXT        NOT NULL REFERENCES tenants(id),
    units       INT         NOT NULL CHECK (units > 0),
    source      TEXT,
    metadata    JSONB       DEFAULT '{}',
    recorded_at TIMESTAMP   DEFAULT CURRENT_TIMESTAMP
);

-- Daily rollup for efficient usage analytics queries.
CREATE TABLE usage_daily (
    tenant_id   TEXT        NOT NULL REFERENCES tenants(id),
    license_id  INT         NOT NULL REFERENCES licenses(id),
    date        DATE        NOT NULL,
    units       INT         NOT NULL CHECK (units >= 0),
    PRIMARY KEY (tenant_id, license_id, date)
);

-- Audit log: Immutable event log (no UPDATE/DELETE in application code).
-- tenant_id is nullable to support system-level events without a tenant.
CREATE TABLE audit_log (
    id              TEXT        PRIMARY KEY,
    tenant_id       TEXT        REFERENCES tenants(id),
    actor_id        TEXT        NOT NULL,
    actor_ip        TEXT        NOT NULL,
    event           TEXT        NOT NULL,
    resource_id     TEXT,
    resource_type   TEXT,
    outcome         TEXT        NOT NULL,
    severity        TEXT        DEFAULT 'info',
    meta            JSONB,
    created_at      TIMESTAMP   DEFAULT CURRENT_TIMESTAMP
);

-- Webhooks: Tenant-registered event delivery endpoints.
-- EC-09: secret_enc stores AES-256-GCM encrypted secret (NOT a hash).
CREATE TABLE webhooks (
    id                  TEXT        PRIMARY KEY,
    tenant_id           TEXT        NOT NULL REFERENCES tenants(id),
    url                 TEXT        NOT NULL,
    events              TEXT[]      NOT NULL,
    secret_enc          BYTEA       NOT NULL,
    is_active           BOOLEAN     DEFAULT TRUE,
    last_triggered_at   TIMESTAMP,
    failure_count       INT         DEFAULT 0,
    created_at          TIMESTAMP   DEFAULT CURRENT_TIMESTAMP
);

-- Webhook delivery log: one row per dispatch attempt.
CREATE TABLE webhook_deliveries (
    id              TEXT        PRIMARY KEY,
    webhook_id      TEXT        NOT NULL REFERENCES webhooks(id),
    event           TEXT        NOT NULL,
    payload         JSONB       NOT NULL,
    attempt         INT         DEFAULT 1,
    status          TEXT        NOT NULL   CHECK (status IN ('pending','success','failed')),
    response_code   INT,
    error           TEXT,
    next_retry_at   TIMESTAMP,
    delivered_at    TIMESTAMP,
    created_at      TIMESTAMP   DEFAULT CURRENT_TIMESTAMP
);

-- Tenant signing key overrides (optional, for white-label).
CREATE TABLE tenant_signing_keys (
    id                      TEXT        PRIMARY KEY,
    tenant_id               TEXT        NOT NULL UNIQUE REFERENCES tenants(id),
    kid                     TEXT        NOT NULL,
    public_key_pem          TEXT        NOT NULL,
    private_key_encrypted   TEXT        NOT NULL,
    algorithm               TEXT        DEFAULT 'ed25519',
    is_active               BOOLEAN     DEFAULT TRUE,
    created_at              TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    retired_at              TIMESTAMP
);

-- =============================================================================
-- Indexes
-- =============================================================================

-- Tenants
-- Unique index for O(1) auth lookup by hashed API key.
CREATE UNIQUE INDEX idx_tenant_api_key_hash     ON tenants (api_key_hash);
CREATE        INDEX idx_tenant_old_key_hash     ON tenants (old_api_key_hash) WHERE old_api_key_hash IS NOT NULL;

-- Licenses
-- Partial index for soft-delete: active-only queries skip deleted rows.
CREATE        INDEX idx_license_active          ON licenses (tenant_id)           WHERE deleted_at IS NULL;
CREATE        INDEX idx_license_expiry          ON licenses (expires_at)          WHERE deleted_at IS NULL;
CREATE        INDEX idx_license_tenant_status_expiry
              ON licenses (tenant_id, status, expires_at)                         WHERE deleted_at IS NULL;

-- Activations
-- Partial unique: prevents duplicate active client_id on the same license.
CREATE UNIQUE INDEX idx_unique_activation_client
              ON activations (license_id, client_id)    WHERE is_active = TRUE;
CREATE        INDEX idx_activation_license
              ON activations (license_id)               WHERE is_active = TRUE;
CREATE        INDEX idx_activation_active
              ON activations (tenant_id)                WHERE is_active = TRUE;
CREATE        INDEX idx_activation_tenant_license
              ON activations (tenant_id, license_id);

-- Usage
CREATE        INDEX idx_usage_tenant_license    ON usage_records (tenant_id, license_id);
CREATE        INDEX idx_usage_license_time      ON usage_records (license_id, recorded_at);
CREATE        INDEX idx_usage_daily_tenant_date ON usage_daily    (tenant_id, date);

-- Audit
CREATE        INDEX idx_audit_tenant            ON audit_log (tenant_id, created_at DESC);

-- Webhooks
CREATE        INDEX idx_webhook_tenant          ON webhooks (tenant_id);
CREATE        INDEX idx_delivery_retry          ON webhook_deliveries (next_retry_at) WHERE status = 'pending';

-- Tenant signing keys
CREATE        INDEX idx_signing_key_tenant      ON tenant_signing_keys (tenant_id)    WHERE is_active = TRUE;

-- Products
CREATE        INDEX idx_products_tenant         ON products (tenant_id);
CREATE UNIQUE INDEX uq_products_tenant_code     ON products (tenant_id, code);

COMMIT;
