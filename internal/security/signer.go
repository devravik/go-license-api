package crypto

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

// LicensePayload is the data embedded in a signed license file.
// Includes NotBefore for replay/clock-skew protection.
type LicensePayload struct {
	LicenseID  string    `json:"license_id"`
	LicenseKey string    `json:"license_key"`
	Type       string    `json:"type"`
	TenantID   string    `json:"tenant_id"`
	PlanID     string    `json:"plan_id,omitempty"`
	ProductID  string    `json:"product_id,omitempty"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	NotBefore  time.Time `json:"not_before"`
	IssuedAt   time.Time `json:"issued_at"`
	SeatsTotal int       `json:"seats_total"`
	SeatsUsed  int       `json:"seats_used"`
	Features   []string  `json:"features"`
	Kid        string    `json:"kid"`
	Issuer     string    `json:"issuer"`
}

// SignedLicense is a detached-signature envelope.
// Payload contains the exact canonical JSON string of LicensePayload.
type SignedLicense struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
	Kid       string `json:"kid"`
	Issuer    string `json:"issuer"`
}

// ed25519Signer implements License signing using Ed25519.
type ed25519Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	kid        string
	issuer     string
}

func NewEd25519Signer(privateKey ed25519.PrivateKey, kid, issuer string) *ed25519Signer {
	return &ed25519Signer{
		privateKey: privateKey,
		publicKey:  privateKey.Public().(ed25519.PublicKey),
		kid:        kid,
		issuer:     issuer,
	}
}

func GenerateEd25519KeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}

// Sign produces a detached-signature envelope using a canonical JSON representation.
func (s *ed25519Signer) Sign(_ context.Context, license *domain.License) ([]byte, error) {
	now := time.Now()
	payload := &LicensePayload{
		LicenseID:  license.ID,
		LicenseKey: license.Key,
		Type:       license.Type,
		TenantID:   license.TenantID,
		Status:     license.Status,
		Features:   license.FinalFeatures,
		IssuedAt:   now,
		NotBefore:  now.Add(-30 * time.Second),
		Kid:        s.kid,
		Issuer:     s.issuer,
	}
	if license.PlanID != nil {
		payload.PlanID = *license.PlanID
	} else if license.Plan != "" {
		payload.PlanID = license.Plan
	}
	if license.ProductID != nil {
		payload.ProductID = *license.ProductID
	} else if license.Product != "" {
		payload.ProductID = license.Product
	}
	if len(payload.Features) == 0 {
		payload.Features = license.Features
	}
	if license.ExpiresAt != nil {
		payload.ExpiresAt = *license.ExpiresAt
	}
	if license.SeatsTotal != 0 {
		payload.SeatsTotal = license.SeatsTotal
	} else if license.SeatCount != nil {
		payload.SeatsTotal = *license.SeatCount
	}
	payload.SeatsUsed = license.SeatsUsed

	data, err := marshalStableJSON(payload)
	if err != nil {
		return nil, err
	}

	sig := ed25519.Sign(s.privateKey, data)
	env := &SignedLicense{
		Payload:   string(data),
		Signature: base64.StdEncoding.EncodeToString(sig),
		Kid:       s.kid,
		Issuer:    s.issuer,
	}
	return marshalStableJSON(env)
}

func (s *ed25519Signer) PublicKey() ed25519.PublicKey { return s.publicKey }
func (s *ed25519Signer) Kid() string                   { return s.kid }
func (s *ed25519Signer) Issuer() string                { return s.issuer }

func marshalStableJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := b.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}

