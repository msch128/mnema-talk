//go:build integration

package mailtest

import (
	"net/smtp"
	"testing"
)

func TestServerUnstuffsLeadingDots(t *testing.T) {
	s := Start(t)
	addr := s.ln.Addr().String()
	msg := "Subject: dots\r\n\r\nline one\r\n.hidden\r\n..twice\r\n"
	if err := smtp.SendMail(addr, nil, "a@example.org", []string{"b@example.org"}, []byte(msg)); err != nil {
		t.Fatal(err)
	}
	got := s.WaitFor(t, 1)[0]
	if want := "line one\n.hidden\n..twice\n"; got.Body != want {
		t.Fatalf("body = %q, want %q", got.Body, want)
	}
}
