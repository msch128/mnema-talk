package auth

import "testing"

func TestCustomInviteCodesMustBeHardToGuess(t *testing.T) {
	for _, code := range []string{"welcome", "welcome-2026", "mnema2026", "abcdefghij", "k7Rm2xQv9pL", "aaaaaaaaaaaa", "passwort12345", "short9", "OnlyLetter"} {
		if _, err := validateOrGenerateCode(code); err == nil {
			t.Errorf("custom code %q was accepted", code)
		}
	}
	for _, code := range []string{"k7Rm2xQv9pLw", "River-Stone-Lantern-Moss", "q3zv8hn2wb5c", "welcome-to-the-club", "k7Rm2xQv9pLw1"} {
		if got, err := validateOrGenerateCode(code); err != nil || got != code {
			t.Errorf("custom code %q: got %q, %v", code, got, err)
		}
	}
}

func TestGeneratedCodesAreNeverFlagged(t *testing.T) {
	for i := 0; i < 500; i++ {
		code, err := GenerateInviteCode()
		if err != nil || IsWeakInviteCode(code) {
			t.Fatalf("generated %q, %v", code, err)
		}
	}
}

func TestExistingWeakCodesAreFlagged(t *testing.T) {
	if !IsWeakInviteCode("welcome") || IsWeakInviteCode("k7Rm2xQv9pLw") {
		t.Fatal("weak flag wrong")
	}
}
