ALTER TABLE activations
DROP CONSTRAINT IF EXISTS activations_license_id_machine_id_key;

DROP INDEX IF EXISTS idx_activation_machine;

CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_activation
ON activations (license_id, machine_id)
WHERE is_active = TRUE;
