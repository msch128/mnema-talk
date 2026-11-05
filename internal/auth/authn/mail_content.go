package authn

import (
	"context"
	"net/http"
	"strings"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/auth/mailtext"
	"github.com/msch128/mnema-talk/internal/mailer"
)

// The mails are short and plain text; their wording and language rule live in
// package mailtext. The links carry the token in the URL fragment, which
// browsers never send to the server and never put in a Referer header or an
// access log; the web app reads it and posts it.
const (
	verifyPath = "/verify-email"
	resetPath  = "/reset-password"
)

func (h *Handler) link(path, token string) string {
	return strings.TrimRight(h.PublicURL, "/") + path + "#token=" + token
}

// mailLocale picks the language of an account mail (see mailtext.Locale).
func mailLocale(stored, acceptLanguage string) string {
	return mailtext.Locale(stored, acceptLanguage, auth.SupportedLocales)
}

// requestLanguage is the Accept-Language header of r, to be handed to mailLocale.
func requestLanguage(r *http.Request) string { return r.Header.Get("Accept-Language") }

func (h *Handler) verifyMail(ctx context.Context, to, locale, username, token string) mailer.Message {
	return mailtext.Verify(to, locale, h.Names.Get(ctx), username, h.link(verifyPath, token))
}

func (h *Handler) resetMail(ctx context.Context, to, locale, username, token string) mailer.Message {
	return mailtext.Reset(to, locale, h.Names.Get(ctx), username, h.link(resetPath, token))
}

func (h *Handler) alertMail(ctx context.Context, to, locale, username string) mailer.Message {
	return mailtext.Alert(to, locale, h.Names.Get(ctx), username)
}

// TestMail is the message the Owner's "send a test mail" action sends; name
// is the community's.
func TestMail(to, locale, name string) mailer.Message { return mailtext.Test(to, locale, name) }
