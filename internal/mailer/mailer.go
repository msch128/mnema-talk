// Package mailer sends the few plain-text e-mails the server needs (address
// confirmation, password reset, a test message) through the operator's SMTP
// server. Without SMTP_HOST there is no Mailer and nothing is ever sent.
//
// Delivery errors name the stage that failed (dial, tls, auth, ...) and never
// carry server replies or credentials, so they are safe to show to the Owner.
package mailer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
)

// sendTimeout bounds one delivery from dialing to the server's last answer.
const sendTimeout = 20 * time.Second

// Message is one plain-text e-mail.
type Message struct {
	To      string
	Subject string
	Body    string
	// FromName is the sender's display name, normally the community's. It
	// replaces the configured one only when that is empty or the product
	// default, so an operator's own choice in SMTP_FROM stays in force.
	FromName string
}

// defaultFromName is the display name an operator gets by copying the example.
const defaultFromName = "Mnema Talk"

// sender is the From address of msg.
func (m *Mailer) sender(msg Message) string {
	if msg.FromName == "" || (m.from.Name != "" && m.from.Name != defaultFromName) {
		return m.from.String()
	}
	return (&mail.Address{Name: msg.FromName, Address: m.from.Address}).String()
}

// SendError says at which stage a delivery failed.
type SendError struct {
	Stage string
	err   error
}

func (e *SendError) Error() string { return "mail delivery failed (" + e.Stage + ")" }

// Unwrap exposes the cause for logs; it must not be shown to clients.
func (e *SendError) Unwrap() error { return e.err }

// fail wraps err. The cause (it may quote an address or a server reply) is
// logged at debug level only, for the operator who is tracing a mail problem.
func fail(stage string, err error) error {
	slog.Debug("smtp delivery failed", "stage", stage, "cause", err)
	return &SendError{Stage: stage, err: err}
}

// Mailer delivers messages through one SMTP server.
type Mailer struct {
	cfg  config.SMTPConfig
	from *mail.Address
	// tlsConfig replaces the default client configuration (tests trust a
	// throw-away certificate through it).
	tlsConfig *tls.Config
}

// New returns a Mailer for cfg, or nil when mail is not configured.
func New(cfg config.SMTPConfig) (*Mailer, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, errors.New("invalid sender address")
	}
	return &Mailer{cfg: cfg, from: from}, nil
}

// Enabled reports whether m can send; a nil Mailer cannot.
func (m *Mailer) Enabled() bool { return m != nil }

// From is the sender address messages carry.
func (m *Mailer) From() string { return m.from.String() }

// TLS is the transport security mode in use (starttls, tls or none).
func (m *Mailer) TLS() string { return m.cfg.TLS }

// Send delivers msg. The recipient must be a bare address; anything that
// could add headers or recipients is refused before a connection is made.
func (m *Mailer) Send(ctx context.Context, msg Message) error {
	to, err := recipient(msg.To)
	if err != nil {
		return fail("recipient", err)
	}
	if strings.ContainsAny(msg.Subject, "\r\n") {
		return fail("message", errors.New("line break in subject"))
	}
	raw, err := m.compose(to, msg)
	if err != nil {
		return fail("message", err)
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return m.deliver(ctx, to, raw)
}

// recipient accepts "user@host" only (no display name, no list).
func recipient(s string) (string, error) {
	if strings.ContainsAny(s, "\r\n\x00") {
		return "", errors.New("control character in address")
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return "", errors.New("not a plain address")
	}
	return a.Address, nil
}

func (m *Mailer) deliver(ctx context.Context, to string, raw []byte) error {
	conn, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return fail("greeting", err)
	}
	defer c.Close()
	if err := m.secure(c); err != nil {
		return err
	}
	if err := m.transmit(c, to, raw); err != nil {
		return err
	}
	_ = c.Quit()
	return nil
}

// connect opens the TCP connection, wrapped in TLS for the implicit mode.
func (m *Mailer) connect(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fail("connect", err)
	}
	if m.cfg.TLS != config.SMTPImplicit {
		return conn, nil
	}
	t := tls.Client(conn, m.clientTLS())
	if err := t.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fail("tls", err)
	}
	return t, nil
}

func (m *Mailer) clientTLS() *tls.Config {
	if m.tlsConfig != nil {
		return m.tlsConfig.Clone()
	}
	return &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
}

// secure upgrades a plain connection (STARTTLS is required, never optional,
// so a network attacker cannot strip it) and signs in.
func (m *Mailer) secure(c *smtp.Client) error {
	if m.cfg.TLS == config.SMTPStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fail("tls", errors.New("server offers no STARTTLS"))
		}
		if err := c.StartTLS(m.clientTLS()); err != nil {
			return fail("tls", err)
		}
	}
	if m.cfg.Username == "" {
		return nil
	}
	if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
		return fail("auth", err)
	}
	return nil
}

func (m *Mailer) transmit(c *smtp.Client, to string, raw []byte) error {
	if err := c.Mail(m.from.Address); err != nil {
		return fail("sender", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fail("recipient", err)
	}
	w, err := c.Data()
	if err != nil {
		return fail("data", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fail("data", err)
	}
	if err := w.Close(); err != nil {
		return fail("data", err)
	}
	return nil
}

// messageID is a fresh unique id in the sender's domain.
func (m *Mailer) messageID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random id: %w", err)
	}
	at := strings.LastIndex(m.from.Address, "@")
	return "<" + hex.EncodeToString(b) + m.from.Address[at:] + ">", nil
}
