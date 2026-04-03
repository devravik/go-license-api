package crypto

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashAPIKey returns the lowercase hex-encoded SHA-256 digest of the raw API
// key.  Only the hash is ever stored in the database; the plaintext key is
// returned to the caller once on creation / rotation and never persisted.
func HashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}
