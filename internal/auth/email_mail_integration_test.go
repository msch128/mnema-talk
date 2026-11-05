//go:build integration

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/mailer"
)

// refusingMail fails every send.
type refusingMail struct{}

func (refusingMail) Send(context.Context, mailer.Message) error { return errors.New("smtp down") }

func TestNewAddressIsKeptAndReportedWhenTheConfirmationMailIsRefused(t *testing.T) {
	pool, _, member := authFixture(t)
	h := NewHandler(&Sessions{DB: pool, Secret: "export-test-secret-export-test-secret"}, nil)
	h.Mail = refusingMail{}
	w := authRequest(t, h.setEmail, member, `{"email":"ada@example.org","current_password":"`+testPassword+`"}`, uuid.Nil)
	if w.Code != http.StatusOK {
		t.Fatalf("the change was made, so the answer is 200: %d %s", w.Code, w.Body)
	}
	var info AccountInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Email != "ada@example.org" || info.EmailVerified || info.ConfirmationSent == nil || *info.ConfirmationSent {
		t.Fatalf("%+v", info)
	}
}

func TestSpentMailBudgetDoesNotTurnASavedAddressIntoAnError(t *testing.T) {
	pool, _, member := authFixture(t)
	h := NewHandler(&Sessions{DB: pool, Secret: "export-test-secret-export-test-secret"}, nil)
	h.Mail = okMail{}
	for i := 0; i < tokensPerHour; i++ {
		if _, err := issueToken(context.Background(), pool, member.ID, purposeEmailVerify, "old@example.org", EmailVerifyTTL); err != nil {
			t.Fatal(err)
		}
	}
	w := authRequest(t, h.setEmail, member, `{"email":"a6@example.org","current_password":"`+testPassword+`"}`, uuid.Nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var last AccountInfo
	if err := json.Unmarshal(w.Body.Bytes(), &last); err != nil || last.ConfirmationSent == nil || *last.ConfirmationSent {
		t.Fatalf("the sixth mail is over the hourly budget: %+v, %v", last, err)
	}
	var stored string
	if err := pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id = $1`, member.ID).Scan(&stored); err != nil || stored != "a6@example.org" {
		t.Fatalf("stored %q, %v", stored, err)
	}
}

type okMail struct{}

func (okMail) Send(context.Context, mailer.Message) error { return nil }
