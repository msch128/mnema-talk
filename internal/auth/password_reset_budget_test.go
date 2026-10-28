package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/mailer"
)

type noMail struct{}

func (noMail) Send(context.Context, mailer.Message) error { return nil }

// Anyone can type any address into the reset form. That must not use up the
// per-address budget of the confirmation mails of the account that owns it.
func TestResetRequestsDoNotSpendTheConfirmationMailBudget(t *testing.T) {
	h := NewHandler(&Sessions{}, nil)
	h.Mail = noMail{}
	// Fill the mail queue: the jobs are dropped, only the limiters are exercised.
	for range cap(h.rec.slots) {
		h.rec.slots <- struct{}{}
	}
	const email = "member@example.org"
	for range 12 {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"`+email+`"}`))
		w := httptest.NewRecorder()
		if err := h.requestPasswordReset(w, r); err != nil || w.Code != http.StatusAccepted {
			t.Fatalf("request: %v %d", err, w.Code)
		}
	}
	for i := range 5 {
		if ok, _ := h.rec.perAddress.Allow(addressKey(email)); !ok {
			t.Fatalf("confirmation mail %d refused after anonymous reset requests", i+1)
		}
	}
}
