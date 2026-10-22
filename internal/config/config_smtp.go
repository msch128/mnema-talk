package config

import (
	"fmt"
	"net/mail"
	"os"
	"strings"
)

// SMTP transport security modes (SMTP_TLS).
const (
	SMTPStartTLS = "starttls"
	SMTPImplicit = "tls"
	SMTPPlain    = "none"
)

// SMTPConfig is the optional outgoing mail setup. Host empty means no mail:
// everything that needs it (address confirmation, password reset by e-mail)
// is then off and the rest of the server behaves as before. The password
// comes from the environment or a file (SMTP_PASSWORD_FILE) and is never
// stored in the database, so it is not part of a backup of it.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	// From is the sender address, e.g. "Mnema Talk <noreply@example.org>".
	From string
	// TLS is SMTPStartTLS (default, port 587), SMTPImplicit (port 465) or
	// SMTPPlain (no encryption; refused in production).
	TLS string
}

// Enabled reports whether outgoing mail is configured.
func (s SMTPConfig) Enabled() bool { return s.Host != "" }

// smtpSettings reads the SMTP_* variables.
func (r *envReader) smtpSettings() SMTPConfig {
	s := SMTPConfig{
		Host:     r.get("SMTP_HOST", ""),
		Username: r.get("SMTP_USERNAME", ""),
		From:     r.get("SMTP_FROM", ""),
		TLS:      strings.ToLower(r.get("SMTP_TLS", SMTPStartTLS)),
	}
	s.Password = r.secret("SMTP_PASSWORD")
	defaultPort := 587
	if s.TLS == SMTPImplicit {
		defaultPort = 465
	}
	s.Port = r.int("SMTP_PORT", defaultPort)
	return s
}

// secret reads KEY or, when KEY_FILE names a file, the file's first line
// (surrounding white space removed). Setting both is an error, so the value
// in use is never in doubt.
func (r *envReader) secret(key string) string {
	direct := r.get(key, "")
	path := r.get(key+"_FILE", "")
	switch {
	case direct != "" && path != "":
		r.fail(fmt.Errorf("%s and %s_FILE are both set; use one", key, key))
		return ""
	case path == "":
		return direct
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		r.fail(fmt.Errorf("%s_FILE: cannot read the file", key))
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
}

// validateSMTP checks the mail setup when a host is given. Credentials never
// travel unencrypted: SMTP_TLS=none is refused with a username and refused in
// production altogether.
func (c *Config) validateSMTP() error {
	s := c.SMTP
	if s.Host == "" {
		return nil
	}
	if strings.ContainsAny(s.Host, " /\\@") {
		return fmt.Errorf("SMTP_HOST must be a host name or address, got %q", s.Host)
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("SMTP_PORT must be between 1 and 65535")
	}
	if err := c.validateSMTPSecurity(); err != nil {
		return err
	}
	if s.From == "" {
		return fmt.Errorf("SMTP_FROM is required when SMTP_HOST is set")
	}
	if _, err := mail.ParseAddress(s.From); err != nil || strings.ContainsAny(s.From, "\r\n") {
		return fmt.Errorf("SMTP_FROM must be an e-mail address such as noreply@example.org")
	}
	return nil
}

func (c *Config) validateSMTPSecurity() error {
	s := c.SMTP
	switch s.TLS {
	case SMTPStartTLS, SMTPImplicit:
	case SMTPPlain:
		if c.IsProduction() {
			return fmt.Errorf("SMTP_TLS=none is not allowed in production; use starttls or tls")
		}
		if s.Username != "" || s.Password != "" {
			return fmt.Errorf("SMTP_TLS=none cannot be combined with SMTP_USERNAME or SMTP_PASSWORD: credentials would travel unencrypted")
		}
	default:
		return fmt.Errorf("SMTP_TLS must be starttls, tls or none, got %q", s.TLS)
	}
	if (s.Username == "") != (s.Password == "") {
		return fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must be set together")
	}
	if isPlaceholder(s.Password) {
		return fmt.Errorf("SMTP_PASSWORD still uses a public example value")
	}
	return nil
}
