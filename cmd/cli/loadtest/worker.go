package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/devravik/go-license-api/internal/app"
)

type TenantInfo struct {
	ID     string
	APIKey string
}

type LicenseInfo struct {
	Key string
}

type RunConfig struct {
	Mode         string
	BaseURL      string
	Duration     time.Duration
	Workers      int
	RPS          int
	Burst        int
	SkewHotPct   int // e.g. 80 means 20% tenants get 80% traffic
	InvalidRate  float64
	OpValidate   int // percent
	OpActivate   int // percent
	OpUsage      int // percent
	ColdStart    bool
	LowRPSTenants int
	Logging      bool
}

type DirectDeps struct {
	Validation app.ValidationService
	Activation app.ActivationService
}

type Corpus struct {
	Tenants  []TenantInfo
	LicensesByTenant map[string][]LicenseInfo
	RandomLicenses   []LicenseInfo // cross-tenant invalid pool
}

func pickOp(r int, validate, activate, usage int) OpType {
	if r < validate {
		return OpValidate
	}
	if r < validate+activate {
		return OpActivate
	}
	return OpUsage
}

func buildTenantDistribution(tenants []TenantInfo, hotPct int) []TenantInfo {
	if len(tenants) == 0 {
		return tenants
	}
	// 20% tenants receive hotPct% of traffic. Repeat entries to bias selection.
	n := len(tenants)
	hotCount := max(1, n/5)
	hot := tenants[:hotCount]
	cold := tenants[hotCount:]

	var dist []TenantInfo
	hotWeight := hotPct
	coldWeight := 100 - hotPct
	// scale weights roughly: repeat hot tenants more times
	for _, t := range hot {
		for i := 0; i < hotWeight; i++ {
			dist = append(dist, t)
		}
	}
	for _, t := range cold {
		for i := 0; i < coldWeight; i++ {
			dist = append(dist, t)
		}
	}
	if len(dist) == 0 {
		return tenants
	}
	return dist
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func RunWorkers(ctx context.Context, cfg RunConfig, deps *DirectDeps, corpus *Corpus, m *Metrics) {
	// attach cfg to context so performHTTP can log when requested
	ctx = withRunConfig(ctx, cfg)
	if cfg.RPS <= 0 {
		cfg.RPS = 1
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}

	tenantDist := buildTenantDistribution(corpus.Tenants, cfg.SkewHotPct)
	// If no tenants available, nothing to do; avoid panics.
	if len(tenantDist) == 0 {
		return
	}
	// Global token bucket implemented via time-based ticker distributing tokens.
	tokenCh := make(chan struct{}, max(1, cfg.Burst))
	stopTokens := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(max(1, cfg.RPS)))
		defer ticker.Stop()
		var produced int
		for {
			select {
			case <-stopTokens:
				return
			case <-ticker.C:
				// Allow small bursts by capacity; otherwise 1 token per tick.
				select { case tokenCh <- struct{}{}: default: }
				produced++
				_ = produced
			}
		}
	}()

	var started int32
	for i := 0; i < cfg.Workers; i++ {
		go func() {
			atomic.AddInt32(&started, 1)
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			for {
				select {
				case <-ctx.Done():
					return
				case <-tokenCh:
					// pick tenant
					if len(tenantDist) == 0 {
						continue
					}
					t := tenantDist[rng.Intn(len(tenantDist))]
					// pick license
					lics := corpus.LicensesByTenant[t.ID]
					var key string
					if len(lics) > 0 && rng.Float64() >= cfg.InvalidRate {
						key = lics[rng.Intn(len(lics))].Key
					} else {
						// generate invalid/random
						key = randomKey(rng)
					}
					// pick operation
					op := pickOp(rng.Intn(100), cfg.OpValidate, cfg.OpActivate, cfg.OpUsage)
					start := time.Now()
					ok, errClass := perform(ctx, cfg, deps, t, key, op, rng)
					lat := time.Since(start)
					m.Record(op, ok, errClass, lat)
				}
			}
		}()
	}

	// wait for duration
	select {
	case <-ctx.Done():
	case <-time.After(cfg.Duration):
	}
	close(stopTokens)
}

func perform(ctx context.Context, cfg RunConfig, deps *DirectDeps, t TenantInfo, key string, op OpType, rng *rand.Rand) (bool, ErrorClass) {
	switch cfg.Mode {
	case "direct":
		return performDirect(ctx, deps, t, key, op, rng)
	default:
		return performHTTP(ctx, cfg.BaseURL, t, key, op, rng)
	}
}

func performDirect(ctx context.Context, deps *DirectDeps, t TenantInfo, key string, op OpType, rng *rand.Rand) (bool, ErrorClass) {
	switch op {
	case OpValidate:
		res, err := deps.Validation.Validate(ctx, t.ID, t.APIKey, key, "")
		if res == nil {
			detail := "result=nil"
			if err != nil {
				detail += fmt.Sprintf(" err=%v", err)
			}
			logError(ctx, op, t.ID, key, detail)
			return false, ErrInternal
		}
		if !res.Valid {
			switch res.Error {
			case "license_expired":
				return false, ErrExpired
			case "license_revoked":
				return false, ErrRevoked
			case "license_not_found":
				return false, ErrNotFound
			case "tenant_suspended", "invalid_tenant", "invalid_key", "invalid_product":
				return false, ErrInvalid
			default:
				return false, ErrInvalid
			}
		}
		return true, ErrNone
	case OpActivate:
		_, _, err := deps.Activation.Activate(ctx, t.ID, key, "machine-"+randomKey(rng), "")
		if err != nil {
			ec := classifyErr(err)
			if ec == ErrInternal || ec == ErrTimeout {
				logError(ctx, op, t.ID, key, fmt.Sprintf("err=%v", err))
			}
			return false, ec
		}
		return true, ErrNone
	case OpUsage:
		// For direct mode, we need license ID; skip lookup and just treat usage as success
		// because activations/usage require repository access. We simulate as a no-op here.
		return true, ErrNone
	default:
		return false, ErrInvalid
	}
}

func classifyErr(err error) ErrorClass {
	// Simplified mapping; HTTP mode has richer signal via status codes.
	msg := err.Error()
	switch {
	case contains(msg, "rate"):
		return ErrRateLimited
	case contains(msg, "expired"):
		return ErrExpired
	case contains(msg, "revoked"):
		return ErrRevoked
	case contains(msg, "not_found"):
		return ErrNotFound
	case contains(msg, "timeout"):
		return ErrTimeout
	default:
		return ErrInvalid
	}
}

func performHTTP(ctx context.Context, baseURL string, t TenantInfo, key string, op OpType, rng *rand.Rand) (bool, ErrorClass) {
	var endpoint string
	var payload any
	switch op {
	case OpValidate:
		endpoint = "/licenses/validate"
		payload = map[string]any{"key": key}
	case OpActivate:
		endpoint = "/licenses/activate"
		payload = map[string]any{"key": key, "machine_id": "machine-" + randomKey(rng)}
	case OpUsage:
		endpoint = "/licenses/usage"
		payload = map[string]any{"key": key, "units": 1 + rng.Intn(5)}
	default:
		return false, ErrInvalid
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+endpoint, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", t.ID)
	req.Header.Set("X-API-Key", t.APIKey)
	// Optional curl logging for reproducibility
	if cfg, ok := ctx.Value(runConfigCtxKey{}).(RunConfig); ok && cfg.Logging {
		curl := "curl -i -X POST " +
			shellQuote(baseURL+endpoint) + " " +
			"-H 'Content-Type: application/json' " +
			"-H 'X-Tenant-ID: " + t.ID + "' " +
			"-H 'X-API-Key: " + t.APIKey + "' " +
			"-d " + shellQuote(string(b))
		log.Println(curl)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logError(ctx, op, t.ID, key, fmt.Sprintf("status=network err=%v", err))
		return false, ErrTimeout
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, ErrNone
	}
	// Classify and log important errors (always, regardless of --logging flag).
	var errClass ErrorClass
	switch resp.StatusCode {
	case 401, 403:
		errClass = ErrInvalid
	case 404:
		errClass = ErrNotFound
	case 402:
		errClass = ErrExpired
	case 429:
		errClass = ErrRateLimited
	case 408, 504:
		errClass = ErrTimeout
	default:
		errClass = ErrInternal
	}
	// Log internal, timeout, and network errors — these indicate server-side issues.
	if errClass == ErrInternal || errClass == ErrTimeout {
		body := readBodyLimited(resp.Body)
		logError(ctx, op, t.ID, key,
			fmt.Sprintf("status=%d endpoint=%s body=%s", resp.StatusCode, endpoint, body))
	}
	return false, errClass
}

// Minimal context plumbing to fetch RunConfig in performHTTP without changing many signatures.
type runConfigCtxKey struct{}

func withRunConfig(ctx context.Context, cfg RunConfig) context.Context {
	return context.WithValue(ctx, runConfigCtxKey{}, cfg)
}

// Error logger: always-on logging for internal/fatal/timeout errors regardless of --logging flag.
type errLogCtxKey struct{}

func withErrLog(ctx context.Context, l *log.Logger) context.Context {
	return context.WithValue(ctx, errLogCtxKey{}, l)
}

func logError(ctx context.Context, op OpType, tenant, key, detail string) {
	l, _ := ctx.Value(errLogCtxKey{}).(*log.Logger)
	if l == nil {
		return
	}
	l.Printf("op=%s tenant=%s key=%s %s", op, tenant, key, detail)
}

// readBodyLimited reads up to 512 bytes from r for error logging.
func readBodyLimited(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 512))
	return string(b)
}

// shellQuote wraps a string for safe shell logging; simplistic implementation.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
func randomKey(rng *rand.Rand) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	for i := range b {
		b[i] = letters[rng.Intn(len(letters))]
	}
	return string(b)
}

func contains(s, sub string) bool {
	s = strings.ToLower(s)
	sub = strings.ToLower(sub)
	return strings.Contains(s, sub)
}

