package crypto

import (
	"context"
	"encoding/base64"
	"sync"
)

// JWKEntry represents a single public key in the JWKS response.
type JWKEntry struct {
	Kid    string `json:"kid"`
	Issuer string `json:"issuer"`
	Kty    string `json:"kty"` // "OKP" for Ed25519
	Crv    string `json:"crv"` // "Ed25519"
	X      string `json:"x"`   // base64url-encoded public key
	Alg    string `json:"alg"` // "EdDSA"
}

// JWKSet is the full JWKS response body.
type JWKSet struct {
	Keys []JWKEntry `json:"keys"`
}

// SignerRegistry resolves the correct signer per tenant.
// Falls back to the global signer if no tenant override is registered.
type SignerRegistry struct {
	mu      sync.RWMutex
	global  *ed25519Signer
	tenants map[string]*ed25519Signer
}

func NewSignerRegistry(global *ed25519Signer) *SignerRegistry {
	return &SignerRegistry{
		global:  global,
		tenants: make(map[string]*ed25519Signer),
	}
}

// For returns the signer for a tenant. Falls back to global.
func (r *SignerRegistry) For(_ context.Context, tenantID string) *ed25519Signer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.tenants[tenantID]; ok {
		return s
	}
	return r.global
}

// RegisterTenant sets a custom signing key for a tenant.
func (r *SignerRegistry) RegisterTenant(tenantID string, signer *ed25519Signer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tenants[tenantID] = signer
}

// JWKS returns all active public keys as a JWKSet for /.well-known/jwks.json.
func (r *SignerRegistry) JWKS() *JWKSet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := &JWKSet{}
	set.Keys = append(set.Keys, signerToJWK(r.global))
	for _, s := range r.tenants {
		set.Keys = append(set.Keys, signerToJWK(s))
	}
	return set
}

func signerToJWK(s *ed25519Signer) JWKEntry {
	return JWKEntry{
		Kid:    s.kid,
		Issuer: s.issuer,
		Kty:    "OKP",
		Crv:    "Ed25519",
		X:      base64.RawURLEncoding.EncodeToString(s.publicKey),
		Alg:    "EdDSA",
	}
}
