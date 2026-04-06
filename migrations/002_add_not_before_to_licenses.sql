ALTER TABLE licenses
ADD COLUMN IF NOT EXISTS not_before TIMESTAMP;

ALTER TABLE licenses
ADD CONSTRAINT licenses_not_before_before_expiry_chk
CHECK (not_before IS NULL OR expires_at IS NULL OR not_before <= expires_at);
