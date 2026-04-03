-- Migrate activations from machine_id to client_id while keeping backward compatibility.
BEGIN;

-- 1) Add client_id column if not exists
ALTER TABLE activations
ADD COLUMN IF NOT EXISTS client_id TEXT;

-- 2) Backfill client_id from machine_id for existing rows
UPDATE activations
SET client_id = machine_id
WHERE client_id IS NULL;

-- 3) Create partial unique index on (license_id, client_id) for active rows
DROP INDEX IF EXISTS idx_unique_activation;
DROP INDEX IF EXISTS idx_activation_machine;
CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_activation_client
ON activations (license_id, client_id)
WHERE is_active = TRUE;

-- 4) Optional: helper index for lookups by client_id
CREATE INDEX IF NOT EXISTS idx_activation_client
ON activations (license_id, client_id)
WHERE is_active = TRUE;

COMMIT;
