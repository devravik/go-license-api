package crypto

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

func TestEd25519Signer_SignAndVerify(t *testing.T) {
	pub, priv, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("gen keys: %v", err)
	}
	s := NewEd25519Signer(priv, "kid-1", "issuer-1")

	expires := time.Now().Add(24 * time.Hour)
	lic := &domain.License{
		TenantID:  "t1",
		Key:       "LIC-1",
		Product:   "pro",
		Plan:      "starter",
		ExpiresAt: &expires,
		Features:  []string{"sso"},
	}

	envJSON, err := s.Sign(context.Background(), lic)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	var env SignedLicense
	if err := json.Unmarshal(envJSON, &env); err != nil {
		t.Fatalf("unmarshal env: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	// Verify with public key
	if !ed25519.Verify(pub, []byte(env.Payload), sig) {
		t.Fatalf("signature did not verify")
	}

	// Tamper payload: change a byte and expect verification to fail.
	payload := []byte(env.Payload)
	if len(payload) > 0 {
		payload[0] ^= 0x01
	}
	if ed25519.Verify(pub, payload, sig) {
		t.Fatalf("expected tampered payload to fail verification")
	}
}
