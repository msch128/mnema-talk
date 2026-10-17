package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBanExpiryNeedsADatabase(t *testing.T) {
	done := make(chan struct{})
	go func() {
		(&Handler{}).RunBanExpiry(context.Background(), time.Hour)
		(&Handler{Sessions: &Sessions{}}).RunBanExpiry(context.Background(), time.Hour)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunBanExpiry without a database must return at once")
	}
}

func TestReasonsAreCleaned(t *testing.T) {
	if got, err := cleanReason("  spam  "); err != nil || got != "spam" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := cleanReason(strings.Repeat("x", MaxReasonLen+1)); err == nil {
		t.Fatal("overlong reason accepted")
	}
	if got, err := cleanReason(""); err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestActiveConditionsQualifyTheTable(t *testing.T) {
	if got := ActiveSQL(""); !strings.HasPrefix(got, "NOT (disabled_at IS NOT NULL") || !strings.Contains(got, "banned_until > NOW()") {
		t.Fatal(got)
	}
	if got := InactiveSQL("u"); !strings.Contains(got, "u.disabled_at") || !strings.Contains(got, "u.banned_at") || strings.Contains(got, " banned_at") {
		t.Fatal(got)
	}
}
