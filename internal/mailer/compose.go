package mailer

import (
	"bytes"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"
)

// compose renders the RFC 5322 message: UTF-8 subject and a quoted-printable
// plain-text body, with CRLF line ends.
func (m *Mailer) compose(to string, msg Message) ([]byte, error) {
	id, err := m.messageID()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	header := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	header("From", m.sender(msg))
	header("To", to)
	header("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header("Date", time.Now().UTC().Format(time.RFC1123Z))
	header("Message-ID", id)
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=UTF-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	// Mail that is an automatic reply to nothing: keep auto-responders quiet.
	header("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	w := quotedprintable.NewWriter(&b)
	if _, err := w.Write([]byte(crlf(msg.Body))); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// crlf normalises every line break to CRLF.
func crlf(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}
