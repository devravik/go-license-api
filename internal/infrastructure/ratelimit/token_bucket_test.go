package ratelimit

import (
	"testing"
	"time"
)

func TestBucket_AllowBurst(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := NewBucketWithNow(10, 2, func() time.Time { return fixed })

	if !b.Allow() {
		t.Fatal("expected allow (1/2 of burst)")
	}
	if !b.Allow() {
		t.Fatal("expected allow (2/2 of burst)")
	}
	if b.Allow() {
		t.Fatal("expected reject after burst is exhausted")
	}
}

func TestBucket_RefillAfterTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }

	b := NewBucketWithNow(1, 2, nowFn) // rps=1 => 1 token per second

	// Exhaust burst.
	if !b.Allow() || !b.Allow() {
		t.Fatal("expected allow for burst exhaustion")
	}
	if b.Allow() {
		t.Fatal("expected reject before refill")
	}

	// Refill 1 token after 1 second.
	now = now.Add(1 * time.Second)
	if !b.Allow() {
		t.Fatal("expected allow after refill")
	}
	if b.Allow() {
		t.Fatal("expected reject when refill is not sufficient")
	}
}

func TestBucket_CapsMaxBurst(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }

	b := NewBucketWithNow(1, 2, nowFn) // maxBurst=2

	// Exhaust burst at T0.
	if !b.Allow() || !b.Allow() {
		t.Fatal("expected allow for burst exhaustion")
	}
	if b.Allow() {
		t.Fatal("expected reject before refill")
	}

	// Jump forward 10s; tokens should cap at maxBurst=2.
	now = now.Add(10 * time.Second)
	if !b.Allow() || !b.Allow() {
		t.Fatal("expected allow for capped refill tokens")
	}
	if b.Allow() {
		t.Fatal("expected reject after capped tokens are exhausted")
	}
}

