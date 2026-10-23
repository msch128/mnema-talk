package auth

import (
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

const serviceName = "Mnema Talk"

func (h *Handler) verifyMail(to, locale, username, token string) mailer.Message {
	link, name := h.link(verifyPath, token), serviceName
	if locale == "de" {
		return mailer.Message{To: to, Subject: name + ": E-Mail-Adresse bestätigen", Body: fmt.Sprintf(
			"Hallo %s,\n\nbitte bestätige diese E-Mail-Adresse für dein Konto bei %s:\n\n%s\n\n"+
				"Der Link gilt 24 Stunden und kann einmal benutzt werden. Du hast das nicht angefordert? "+
				"Dann ignoriere diese Nachricht, es passiert nichts.\n", username, name, link)}
	}
	return mailer.Message{To: to, Subject: name + ": confirm your e-mail address", Body: fmt.Sprintf(
		"Hello %s,\n\nplease confirm this e-mail address for your account at %s:\n\n%s\n\n"+
			"The link works once and for 24 hours. You did not ask for this? "+
			"Ignore this message; nothing happens.\n", username, name, link)}
}

func (h *Handler) resetMail(to, locale, username, token string) mailer.Message {
	link, name := h.link(resetPath, token), serviceName
	if locale == "de" {
		return mailer.Message{To: to, Subject: name + ": Passwort zurücksetzen", Body: fmt.Sprintf(
			"Hallo %s,\n\nfür dein Konto bei %s wurde ein neues Passwort angefordert. Mit diesem Link legst du es fest:\n\n%s\n\n"+
				"Der Link gilt 30 Minuten und kann einmal benutzt werden. Beim Zurücksetzen werden alle angemeldeten Geräte abgemeldet. "+
				"Du hast das nicht angefordert? Dann ignoriere diese Nachricht; dein Passwort bleibt, wie es ist.\n", username, name, link)}
	}
	return mailer.Message{To: to, Subject: name + ": reset your password", Body: fmt.Sprintf(
		"Hello %s,\n\na new password was requested for your account at %s. Use this link to choose it:\n\n%s\n\n"+
			"The link works once and for 30 minutes. Resetting signs out every device. "+
			"You did not ask for this? Ignore this message; your password stays as it is.\n", username, name, link)}
}
