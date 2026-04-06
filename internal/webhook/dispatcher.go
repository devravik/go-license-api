package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
	"github.com/devravik/go-license-api/internal/infrastructure/idgen"
	crypto "github.com/devravik/go-license-api/internal/security"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Dispatcher struct {
	db     *pgxpool.Pool
	client *http.Client
	events chan dispatchJob
	encKey []byte
	ctx    context.Context
	cancel context.CancelFunc

	cache map[string][]*domain.Webhook
	mu    sync.RWMutex
}

type EventPayload struct {
	ID         string `json:"id"`
	Event      string `json:"event"`
	Version    string `json:"version"` // payload schema version
	TenantID   string `json:"tenant_id"`
	OccurredAt string `json:"occurred_at"`
	Data       any    `json:"data"`
}

type dispatchJob struct {
	webhook *domain.Webhook
	payload *EventPayload
	attempt int
}

func NewDispatcher(db *pgxpool.Pool, encKey []byte) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dispatcher{
		db:     db,
		client: crypto.NewRestrictedHTTPClient(10 * time.Second),
		events: make(chan dispatchJob, 500),
		encKey: encKey,
		ctx:    ctx,
		cancel: cancel,
		cache:  make(map[string][]*domain.Webhook),
	}
	go d.loop()
	return d
}

// LoadWebhooks populates the in-memory cache (control plane; DB allowed).
func (d *Dispatcher) LoadWebhooks(ctx context.Context) error {
	const q = `SELECT id, tenant_id, url, events, secret_enc, is_active FROM webhooks WHERE is_active = TRUE`
	rows, err := d.db.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("load webhooks: %w", err)
	}
	defer rows.Close()
	tmp := make(map[string][]*domain.Webhook)
	for rows.Next() {
		wh := &domain.Webhook{}
		var events []string
		if err := rows.Scan(&wh.ID, &wh.TenantID, &wh.URL, &events, &wh.SecretEnc, &wh.IsActive); err != nil {
			continue
		}
		wh.Events = events
		tmp[wh.TenantID] = append(tmp[wh.TenantID], wh)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	d.cache = tmp
	d.mu.Unlock()
	return nil
}

func (d *Dispatcher) loop() {
	for {
		select {
		case job := <-d.events:
			d.deliver(job)
		case <-d.ctx.Done():
			return
		}
	}
}

func (d *Dispatcher) Stop() { d.cancel() }

// Dispatch is data-plane path; must be non-blocking and DB-free.
func (d *Dispatcher) Dispatch(_ context.Context, event, tenantID string, data any) {
	d.mu.RLock()
	webhooks := d.cache[tenantID]
	d.mu.RUnlock()
	if len(webhooks) == 0 {
		return
	}
	payloadID, err := idgen.NewID("evt")
	if err != nil {
		return
	}
	payload := &EventPayload{
		ID:         payloadID,
		Event:      event,
		Version:    "v1",
		TenantID:   tenantID,
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Data:       data,
	}
	for _, wh := range webhooks {
		// Filter by subscription
		subscribed := false
		for _, e := range wh.Events {
			if e == event {
				subscribed = true
				break
			}
		}
		if !subscribed {
			continue
		}
		select {
		case d.events <- dispatchJob{webhook: wh, payload: payload, attempt: 1}:
		default:
			// drop on full queue to avoid blocking/buildup
		}
	}
}

func (d *Dispatcher) deliver(job dispatchJob) {
	body, err := json.Marshal(job.payload)
	if err != nil {
		return
	}
	// Enforce max payload size (defense-in-depth, prevents memory pressure)
	const maxPayloadBytes = 256 * 1024 // 256KB
	if len(body) > maxPayloadBytes {
		fmt.Printf("webhook %s blocked: payload too large (%d bytes)\n", job.webhook.ID, len(body))
		return
	}
	// Enforce https at dispatch time and guard against DNS rebinding by relying on restricted transport.
	if u, perr := url.Parse(job.webhook.URL); perr != nil || u.Scheme != "https" {
		fmt.Printf("webhook %s blocked: invalid or non-https URL\n", job.webhook.ID)
		return
	}
	req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, job.webhook.URL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GoLicenseAPI-Webhooks/1.0")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-License-Timestamp", ts)
	req.Header.Set("X-Webhook-Version", "v1")
	req.Header.Set("X-Webhook-Id", job.payload.ID)
	req.Header.Set("X-Webhook-Attempt", strconv.Itoa(job.attempt))

	// Optional content digest for tamper detection
	sum := sha256.Sum256(body)
	req.Header.Set("X-Body-SHA256", hex.EncodeToString(sum[:]))

	secret, err := crypto.DecryptAES(d.encKey, job.webhook.SecretEnc)
	if err != nil {
		fmt.Printf("webhook %s decryption failed: %v\n", job.webhook.ID, err)
		return
	}
	buf := make([]byte, 0, len(ts)+1+len(body))
	buf = append(buf, ts...)
	buf = append(buf, '.')
	buf = append(buf, body...)
	sig := sign(buf, secret)
	// Include signature version for future-proofing
	req.Header.Set("X-License-Signature", "v1="+sig)

	resp, err := d.client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return
		}
	}
	fmt.Printf("webhook delivery failed: id=%s attempt=%d err=%v\n", job.webhook.ID, job.attempt, err)

	if job.attempt < 5 {
		// Explicit retry schedule: 1s → 5s → 25s → 2m → 10m
		schedule := []time.Duration{
			1 * time.Second,
			5 * time.Second,
			25 * time.Second,
			2 * time.Minute,
			10 * time.Minute,
		}
		backoff := schedule[job.attempt-1]
		time.AfterFunc(backoff, func() {
			job.attempt++
			select {
			case d.events <- job:
			default:
			}
		})
	}
}

func sign(payload, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
