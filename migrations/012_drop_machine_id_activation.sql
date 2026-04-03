BEGIN;

-- Permanently remove legacy machine_id column now that client_id is in use.
ALTER TABLE activations
DROP COLUMN IF EXISTS machine_id;

COMMIT;
