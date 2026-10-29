package auth

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/msch128/mnema-talk/internal/mailer"
)

// The mails are short and plain text. The links carry the token in the URL
// fragment, which browsers never send to the server and never put in a
// Referer header or an access log; the web app reads it and posts it.
const (
	verifyPath = "/verify-email"
	resetPath  = "/reset-password"
)

func (h *Handler) link(path, token string) string {
	return strings.TrimRight(h.PublicURL, "/") + path + "#token=" + token
}

// mailJSON holds the subject and body templates by language (see mailLocale).
// A subject takes the service name; a body takes the username, the service
// name and, except for the alert, the link.
//
//go:embed locales/mail.json
var mailJSON []byte

var mailTexts = func() map[string]map[string]string {
	var out map[string]map[string]string
	if err := json.Unmarshal(mailJSON, &out); err != nil {
		panic("auth: locales/mail.json: " + err.Error())
	}
	return out
}()

func mailText(locale, key string) string {
	if t, ok := mailTexts[locale]; ok {
		return t[key]
	}
	return mailTexts["en"][key]
}

// The mails call the service by the community's name (see package community):
// subject, body and the sender's display name.

func (h *Handler) verifyMail(ctx context.Context, to, locale, username, token string) mailer.Message {
	name := h.Names.Get(ctx)
	return mailer.Message{To: to, FromName: name, Subject: fmt.Sprintf(mailText(locale, "verify.subject"), name),
		Body: fmt.Sprintf(mailText(locale, "verify.body"), username, name, h.link(verifyPath, token))}
}

func (h *Handler) resetMail(ctx context.Context, to, locale, username, token string) mailer.Message {
	name := h.Names.Get(ctx)
	return mailer.Message{To: to, FromName: name, Subject: fmt.Sprintf(mailText(locale, "reset.subject"), name),
		Body: fmt.Sprintf(mailText(locale, "reset.body"), username, name, h.link(resetPath, token))}
}

func (h *Handler) alertMail(ctx context.Context, to, locale, username string) mailer.Message {
	name := h.Names.Get(ctx)
	return mailer.Message{To: to, FromName: name, Subject: fmt.Sprintf(mailText(locale, "alert.subject"), name),
		Body: fmt.Sprintf(mailText(locale, "alert.body"), username, name)}
}

// TestMail is the message the Owner's "send a test mail" action sends; name
// is the community's.
func TestMail(to, locale, name string) mailer.Message {
	return mailer.Message{To: to, FromName: name, Subject: fmt.Sprintf(mailText(locale, "test.subject"), name), Body: mailText(locale, "test.body")}
}
