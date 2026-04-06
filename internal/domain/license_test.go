package domain_test

import (
	"testing"
	"time"

	"github.com/devravik/go-license-api/internal/domain"
)

func TestLicense_IsExpired_NoExpiry(t *testing.T) {
	l := &domain.License{ExpiresAt: nil, GracePeriodDays: 0}
	if l.IsExpired() {
		t.Fatalf("expected not expired when no expiry set")
	}
}

func TestLicense_IsExpired_ExpiredNoGrace(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	l := &domain.License{ExpiresAt: &past, GracePeriodDays: 0}
	if !l.IsExpired() {
		t.Fatalf("expected expired when past expiry and no grace")
	}
}

func TestLicense_IsExpired_InGrace(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour)
	l := &domain.License{ExpiresAt: &past, GracePeriodDays: 3}
	if l.IsExpired() {
		t.Fatalf("expected not expired during grace period")
	}
	if !l.IsInGracePeriod() {
		t.Fatalf("expected in grace period")
	}
}

func TestLicense_GraceBoundaries(t *testing.T) {
	now := time.Now()
	exp := now.Add(-1 * time.Hour)
	l := &domain.License{ExpiresAt: &exp, GracePeriodDays: 0}
	if !l.IsExpired() {
		t.Fatalf("expected expired at t+1h without grace")
	}

	// Inside grace window (not near boundary) => not expired
	exp2 := now.Add(-23 * time.Hour)
	l2 := &domain.License{ExpiresAt: &exp2, GracePeriodDays: 1}
	if l2.IsExpired() {
		t.Fatalf("expected not expired during grace window")
	}

	// After grace end => expired
	exp3 := now.Add(-49 * time.Hour)
	l3 := &domain.License{ExpiresAt: &exp3, GracePeriodDays: 2} // grace ends at -1h
	if !l3.IsExpired() {
		t.Fatalf("expected expired after grace end")
	}
}

func TestLicense_FeatureCheck(t *testing.T) {
	l := &domain.License{Features: []string{"sso", "audit", "usage"}}
	if !l.HasFeature("audit") {
		t.Fatalf("expected feature audit to be present")
	}
	if l.HasFeature("unknown") {
		t.Fatalf("did not expect feature unknown to be present")
	}
}
