-- Audit is append-heavy; reduce write amplification by removing low-value index.
-- Keep idx_audit_tenant for tenant-scoped operational queries.

DROP INDEX IF EXISTS idx_audit_event;
