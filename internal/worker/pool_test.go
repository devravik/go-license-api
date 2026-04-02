package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/app"
	"github.com/devravik/go-license-api/internal/domain"
)

type mockValidationService struct {
	validate func(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error)
}

func (m *mockValidationService) Validate(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
	return m.validate(ctx, tenantID, apiKey, key, product)
}

func TestPool_ProcessesJobs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var calls atomic.Int32
	val := &mockValidationService{
		validate: func(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
			calls.Add(1)
			return &domain.ValidationResult{Valid: true, Meta: &domain.ValidationMeta{Plan: "pro"}}, nil
		},
	}

	// Queue must be large enough for this test; we want to validate processing,
	// not queue overflow behavior.
	pool := NewPool(2, 30, app.ValidationService(val))
	pool.Start(ctx)

	const n = 20
	resultCh := make([]chan Result, 0, n)
	for i := 0; i < n; i++ {
		ch := make(chan Result, 1)
		ok := pool.Enqueue(&ValidateJob{
			TenantID:   "t1",
			APIKey:     "tenant-key",
			LicenseKey: "LIC-1",
			Product:    "pro",
			Ctx:        ctx,
			ResultCh:   ch,
		})
		if !ok {
			t.Fatalf("expected enqueue to succeed for job %d", i)
		}
		resultCh = append(resultCh, ch)
	}

	for _, ch := range resultCh {
		select {
		case res := <-ch:
			if res.Err != nil {
				t.Fatalf("expected nil error, got %v", res.Err)
			}
			if !res.Valid {
				t.Fatalf("expected valid result")
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for job result")
		}
	}

	if calls.Load() != n {
		t.Fatalf("expected %d calls, got %d", n, calls.Load())
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()
	pool.Drain(drainCtx)
}

func TestPool_QueueOverflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val := &mockValidationService{
		validate: func(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
			return &domain.ValidationResult{Valid: true}, nil
		},
	}

	// No workers => queue never drained.
	pool := NewPool(0, 1, app.ValidationService(val))
	pool.Start(ctx)

	ch1 := make(chan Result, 1)
	ok1 := pool.Enqueue(&ValidateJob{
		TenantID:   "t1",
		APIKey:     "tenant-key",
		LicenseKey: "LIC-1",
		Product:    "pro",
		Ctx:        ctx,
		ResultCh:   ch1,
	})
	if !ok1 {
		t.Fatalf("expected first enqueue to succeed")
	}

	ch2 := make(chan Result, 1)
	ok2 := pool.Enqueue(&ValidateJob{
		TenantID:   "t1",
		APIKey:     "tenant-key",
		LicenseKey: "LIC-2",
		Product:    "pro",
		Ctx:        ctx,
		ResultCh:   ch2,
	})
	if ok2 {
		t.Fatalf("expected second enqueue to fail when queue is full")
	}
}

func TestPool_ConcurrencyLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const workers = 3
	const jobs = 12

	var inFlight atomic.Int32
	var maxSeen atomic.Int32
	started := make(chan struct{}, workers)
	release := make(chan struct{})

	val := &mockValidationService{
		validate: func(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
			n := inFlight.Add(1)
			for {
				prev := maxSeen.Load()
				if n <= prev || maxSeen.CompareAndSwap(prev, n) {
					break
				}
			}
			// Signal that a worker started processing.
			select {
			case started <- struct{}{}:
			default:
			}

			select {
			case <-release:
				inFlight.Add(-1)
				return &domain.ValidationResult{Valid: true}, nil
			case <-ctx.Done():
				inFlight.Add(-1)
				return nil, ctx.Err()
			}
		},
	}

	pool := NewPool(workers, jobs, app.ValidationService(val))
	pool.Start(ctx)

	resultCh := make([]chan Result, 0, jobs)
	for i := 0; i < jobs; i++ {
		ch := make(chan Result, 1)
		if !pool.Enqueue(&ValidateJob{
			TenantID:   "t1",
			APIKey:     "tenant-key",
			LicenseKey: "LIC",
			Product:    "pro",
			Ctx:        ctx,
			ResultCh:   ch,
		}) {
			t.Fatalf("enqueue failed for job %d", i)
		}
		resultCh = append(resultCh, ch)
	}

	// Wait until we know workers are actively processing.
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for i := 0; i < workers; i++ {
		select {
		case <-started:
		case <-deadline.C:
			t.Fatalf("timed out waiting for workers to start")
		}
	}

	// At this moment, max inflight should not exceed the worker count.
	if maxSeen.Load() != workers {
		t.Fatalf("expected max inflight %d, got %d", workers, maxSeen.Load())
	}

	close(release)

	for _, ch := range resultCh {
		select {
		case res := <-ch:
			if res.Err != nil {
				t.Fatalf("expected nil error, got %v", res.Err)
			}
			if !res.Valid {
				t.Fatalf("expected valid result")
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for job result")
		}
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()
	pool.Drain(drainCtx)
}

func TestPool_TaskTimeoutCancelsCtx(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	val := &mockValidationService{
		validate: func(ctx context.Context, tenantID, apiKey, key, product string) (*domain.ValidationResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	pool := NewPool(1, 4, app.ValidationService(val), 50*time.Millisecond)
	pool.Start(ctx)

	ch := make(chan Result, 1)
	if !pool.Enqueue(&ValidateJob{
		TenantID:   "t1",
		APIKey:     "tenant-key",
		LicenseKey: "LIC",
		Product:    "pro",
		Ctx:        ctx,
		ResultCh:   ch,
	}) {
		t.Fatalf("expected enqueue to succeed")
	}

	start := time.Now()
	select {
	case res := <-ch:
		if res.Err == nil {
			t.Fatalf("expected error due to ctx timeout")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for timed-out job result")
	}

	if time.Since(start) > 300*time.Millisecond {
		t.Fatalf("job did not respect task timeout; took %v", time.Since(start))
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer drainCancel()
	pool.Drain(drainCtx)
}

