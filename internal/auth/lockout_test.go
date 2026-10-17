package auth

import (
	"testing"

	"github.com/google/uuid"
)

func TestClearLoginLockoutScopesToOneAccount(t *testing.T) {
	h := NewHandler(nil, nil)
	uid := uuid.New()
	for i := 0; i < 10; i++ {
		h.loginFailures.RecordFailure("203.0.113.9|max")
		h.loginFailures.RecordFailure("203.0.113.9|anna")
	}
	for i := 0; i < 100; i++ {
		h.accountFailures.RecordFailure("max")
	}
	for i := 0; i < 5; i++ {
		h.passwordFails.RecordFailure(uid.String())
	}
	h.unknownDevice.Allow("max")

	locked := h.lockedAccounts()
	if !locked["max"] || !locked["anna"] || len(locked) != 2 {
		t.Fatalf("locked = %v", locked)
	}

	h.ClearLoginLockout(" Max ", uid)
	if ok, _ := h.loginFailures.IsLockedOut("203.0.113.9|max"); ok {
		t.Error("address lockout of max survived")
	}
	if ok, _ := h.accountFailures.IsLockedOut("max"); ok {
		t.Error("account cap of max survived")
	}
	if ok, _ := h.passwordFails.IsLockedOut(uid.String()); ok {
		t.Error("password lockout survived")
	}
	if ok, _ := h.loginFailures.IsLockedOut("203.0.113.9|anna"); !ok {
		t.Error("other account was cleared")
	}
	if locked := h.lockedAccounts(); locked["max"] || !locked["anna"] {
		t.Fatalf("locked after clear = %v", locked)
	}
	// Nil user id: only the name-keyed state is touched.
	h.ClearLoginLockout("anna", uuid.Nil)
	if len(h.lockedAccounts()) != 0 {
		t.Fatal("anna still locked")
	}
}

func TestClearAllLoginLockouts(t *testing.T) {
	h := NewHandler(nil, nil)
	for i := 0; i < 10; i++ {
		h.loginFailures.RecordFailure("198.51.100.1|a")
		h.loginFailures.RecordFailure("198.51.100.2|b")
	}
	h.loginPerIP.Allow("198.51.100.1")
	h.ClearAllLoginLockouts()
	if len(h.lockedAccounts()) != 0 || h.loginFailures.Len() != 0 || h.accountFailures.Len() != 0 {
		t.Fatal("state survived ClearAllLoginLockouts")
	}
}
