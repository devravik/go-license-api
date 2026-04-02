package domain

import "time"

// Webhook is a registered delivery endpoint for a tenant.
type Webhook struct {
	ID       string   `json:"id"`
	TenantID string   `json:"tenant_id"`
	URL      string   `json:"url"`
	Events   []string `json:"events"`

	// EC-09: SecretEnc holds AES-256-GCM ciphertext; never store plaintext or sha256 hash.
	// The signing secret is recovered by decrypting this field at dispatch time.
	SecretEnc []byte `json:"-"` // never serialize to JSON.

	IsActive        bool       `json:"is_active"`
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`
	FailureCount    int        `json:"failure_count,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// WebhookDelivery records a single dispatch attempt.
type WebhookDelivery struct {
	ID        string `json:"id"`
	WebhookID string `json:"webhook_id"`

	Event   string `json:"event"`
	Payload []byte `json:"payload"`

	Attempt int    `json:"attempt"`
	Status  string `json:"status"` // pending | success | failed

	ResponseCode *int       `json:"response_code"`
	NextRetryAt  *time.Time `json:"next_retry_at"`
	DeliveredAt  *time.Time `json:"delivered_at"`
	CreatedAt    time.Time  `json:"created_at"`

	Error string `json:"error,omitempty"`
}
