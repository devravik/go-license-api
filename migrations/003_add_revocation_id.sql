ALTER TABLE licenses
ADD COLUMN IF NOT EXISTS revocation_id TEXT;

-- Optional uniqueness to make revocation list distribution simpler.
CREATE UNIQUE INDEX IF NOT EXISTS idx_license_revocation_id ON licenses(revocation_id) WHERE deleted_at IS NULL;
