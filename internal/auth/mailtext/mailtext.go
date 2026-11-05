// Package mailtext renders the account e-mails (verification, password reset,
// security alert, test mail) and picks their language. It is pure text: no
// handler, no database, so the texts and the language rule can be tested and
// reviewed apart from the authentication code that sends them.
package mailtext

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/msch128/mnema-talk/internal/mailer"
)

// mailJSON holds the subject and body templates by language (see Locale).
// A subject takes the service name; a body takes the username, the service
// name and, except for the alert, the link.
//
//go:embed locales/mail.json
var mailJSON []byte

var mailTexts = func() map[string]map[string]string {
	var out map[string]map[string]string
	if err := json.Unmarshal(mailJSON, &out); err != nil {
		panic("mailtext: locales/mail.json: " + err.Error())
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
// subject, body and the sender's display name. link is the full URL the
// member opens, token included.

// Verify is the e-mail address confirmation mail.
func Verify(to, locale, name, username, link string) mailer.Message {
	return mailer.Message{To: to, FromName: name, Subject: fmt.Sprintf(mailText(locale, "verify.subject"), name),
		Body: fmt.Sprintf(mailText(locale, "verify.body"), username, name, link)}
}

// Reset is the password reset mail.
func Reset(to, locale, name, username, link string) mailer.Message {
	return mailer.Message{To: to, FromName: name, Subject: fmt.Sprintf(mailText(locale, "reset.subject"), name),
		Body: fmt.Sprintf(mailText(locale, "reset.body"), username, name, link)}
}

// Alert is the security alert mail; it carries no link.
func Alert(to, locale, name, username string) mailer.Message {
	return mailer.Message{To: to, FromName: name, Subject: fmt.Sprintf(mailText(locale, "alert.subject"), name),
		Body: fmt.Sprintf(mailText(locale, "alert.body"), username, name)}
}

// Test is the message the Owner's "send a test mail" action sends; name is
// the community's.
func Test(to, locale, name string) mailer.Message {
	return mailer.Message{To: to, FromName: name, Subject: fmt.Sprintf(mailText(locale, "test.subject"), name), Body: mailText(locale, "test.body")}
}
