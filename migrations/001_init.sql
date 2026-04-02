-- Tenants: API consumers (SaaS customers / organizations)
CREATE TABLE tenants (
    id TEXT PRIMARY KEY,
    api_key TEXT NOT NULL,
    -- EC-08: dual-key rotation — old key stays valid during grace period
    old_api_key TEXT,
    old_key_expires_at TIMESTAMP,
    rps INT DEFAULT 100,
    burst INT DEFAULT 200,
    status TEXT NOT NULL DEFAULT 'active',
    suspended_at TIMESTAMP,
    suspension_reason TEXT,
    ip_allowlist TEXT[] DEFAULT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Licenses: Issued to end-users by a tenant
CREATE TABLE licenses (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    key TEXT NOT NULL,
    product_id TEXT,
    product TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    plan TEXT,
    is_trial BOOLEAN DEFAULT FALSE,
    trial_ends_at TIMESTAMP,
    expires_at TIMESTAMP,
    grace_period_days INT DEFAULT 0,
    seat_count INT DEFAULT NULL,
    max_activations INT DEFAULT NULL,
    usage_limit INT DEFAULT NULL,
    usage_used INT DEFAULT 0,
    features TEXT[] DEFAULT '{}',
    meta JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id, key)
);

-- Activations: Seat-based activation records
CREATE TABLE activations (
    id TEXT PRIMARY KEY,
    license_id INT NOT NULL REFERENCES licenses(id),
    tenant_id TEXT NOT NULL,
    machine_id TEXT,
    hostname TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    activated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMP,
    -- EC-01: prevent duplicate active activations for same machine on same license
    UNIQUE(license_id, machine_id)
);

-- Usage: Consumption-based usage records
CREATE TABLE usage_records (
    id SERIAL PRIMARY KEY,
    license_id INT NOT NULL REFERENCES licenses(id),
    tenant_id TEXT NOT NULL,
    units INT NOT NULL,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Audit: Immutable event log (no UPDATE/DELETE in app)
CREATE TABLE audit_log (
    id TEXT PRIMARY KEY,
    tenant_id TEXT,
    actor_id TEXT NOT NULL,
    actor_ip TEXT NOT NULL,
    event TEXT NOT NULL,
    resource_id TEXT,
    outcome TEXT NOT NULL,
    meta JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Webhooks: Tenant-registered event delivery endpoints
-- EC-09: secret_enc stores AES-256-GCM encrypted secret (NOT a hash)
CREATE TABLE webhooks (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    url TEXT NOT NULL,
    events TEXT[] NOT NULL,
    secret_enc BYTEA NOT NULL,     -- AES-256-GCM encrypted; never store plaintext or hash
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Webhook delivery log: one row per dispatch attempt
CREATE TABLE webhook_deliveries (
    id TEXT PRIMARY KEY,
    webhook_id TEXT NOT NULL REFERENCES webhooks(id),
    event TEXT NOT NULL,
    payload JSONB NOT NULL,
    attempt INT DEFAULT 1,
    status TEXT NOT NULL,            -- pending | success | failed
    response_code INT,
    next_retry_at TIMESTAMP,
    delivered_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Tenant signing key overrides (optional, for white-label)
CREATE TABLE tenant_signing_keys (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL UNIQUE REFERENCES tenants(id),
    kid TEXT NOT NULL,
    public_key_pem TEXT NOT NULL,
    private_key_encrypted TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    retired_at TIMESTAMP
);

-- Indexes (critical for query performance at scale)
CREATE INDEX idx_license_key ON licenses(key);
CREATE INDEX idx_license_tenant ON licenses(tenant_id);
CREATE INDEX idx_license_tenant_key ON licenses(tenant_id, key); -- FOR UPDATE lookup
CREATE INDEX idx_activation_license ON activations(license_id) WHERE is_active = TRUE;
CREATE INDEX idx_activation_machine ON activations(license_id, machine_id) WHERE is_active = TRUE; -- EC-01 dedupe
CREATE INDEX idx_tenant_api_key ON tenants(api_key);          -- hot path for auth
CREATE INDEX idx_tenant_old_key ON tenants(old_api_key) WHERE old_api_key IS NOT NULL; -- EC-08 rotation
CREATE INDEX idx_audit_tenant ON audit_log(tenant_id, created_at DESC);
CREATE INDEX idx_audit_event ON audit_log(event, created_at DESC);
CREATE INDEX idx_webhook_tenant ON webhooks(tenant_id);
CREATE INDEX idx_delivery_retry ON webhook_deliveries(next_retry_at) WHERE status = 'pending';
CREATE INDEX idx_signing_key_tenant ON tenant_signing_keys(tenant_id) WHERE is_active = TRUE;

