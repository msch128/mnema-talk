package mailer

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
)

// smtpServer is a small SMTP server for tests: it speaks just enough of the
// protocol, optionally with STARTTLS or implicit TLS and PLAIN auth, and keeps
// what it received.
type smtpServer struct {
	t        *testing.T
	ln       net.Listener
	cert     tls.Certificate
	pool     *x509.CertPool
	startTLS bool
	implicit bool
	user     string
	pass     string

	mu       sync.Mutex
	mails    []received
	authSeen []string
}

type received struct {
	from, to string
	data     string
}

func newSMTPServer(t *testing.T, mode string) *smtpServer {
	t.Helper()
	cert, pool := selfSigned(t)
	s := &smtpServer{t: t, cert: cert, pool: pool, startTLS: mode == config.SMTPStartTLS, implicit: mode == config.SMTPImplicit}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if s.implicit {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	s.ln = ln
	t.Cleanup(func() { _ = ln.Close() })
	go s.accept()
	return s
}

func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func (s *smtpServer) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

func (s *smtpServer) hostPort() (string, int) {
	addr := s.ln.Addr().(*net.TCPAddr)
	return "localhost", addr.Port
}

func (s *smtpServer) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	say := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	say("220 test ESMTP")
	var cur received
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			s.ehlo(say, conn)
		case "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12})
			if err := tc.Handshake(); err != nil {
				return
			}
			conn = tc
			r = bufio.NewReader(conn)
			say = func(l string) { _, _ = io.WriteString(conn, l+"\r\n") }
		case "AUTH":
			s.auth(line, say)
		case "MAIL":
			cur = received{from: between(line, "<", ">")}
			say("250 ok")
		case "RCPT":
			cur.to = between(line, "<", ">")
			say("250 ok")
		case "DATA":
			say("354 go")
			cur.data = readData(r)
			s.mu.Lock()
			s.mails = append(s.mails, cur)
			s.mu.Unlock()
			say("250 queued")
		case "QUIT":
			say("221 bye")
			return
		default:
			say("500 what")
		}
	}
}

func (s *smtpServer) ehlo(say func(string), conn net.Conn) {
	_, isTLS := conn.(*tls.Conn)
	say("250-test")
	if s.startTLS && !isTLS {
		say("250-STARTTLS")
	}
	if s.user != "" {
		say("250-AUTH PLAIN")
	}
	say("250 8BITMIME")
}

func (s *smtpServer) auth(line string, say func(string)) {
	fields := strings.Fields(line)
	raw, _ := base64.StdEncoding.DecodeString(fields[len(fields)-1])
	parts := strings.Split(string(raw), "\x00")
	s.mu.Lock()
	s.authSeen = append(s.authSeen, strings.Join(parts, "|"))
	s.mu.Unlock()
	if len(parts) == 3 && parts[1] == s.user && parts[2] == s.pass {
		say("235 ok")
		return
	}
	say("535 no")
}

func between(s, open, close string) string {
	i := strings.Index(s, open)
	j := strings.LastIndex(s, close)
	if i < 0 || j < i {
		return ""
	}
	return s[i+1 : j]
}

func readData(r *bufio.Reader) string {
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil || line == ".\r\n" {
			return b.String()
		}
		b.WriteString(line)
	}
}

func (s *smtpServer) config(tlsMode string) config.SMTPConfig {
	host, port := s.hostPort()
	return config.SMTPConfig{Host: host, Port: port, From: "Mnema Talk <noreply@example.org>", TLS: tlsMode, Username: s.user, Password: s.pass}
}

func (s *smtpServer) mailer(tlsMode string) *Mailer {
	m, err := New(s.config(tlsMode))
	if err != nil {
		s.t.Fatal(err)
	}
	m.tlsConfig = &tls.Config{RootCAs: s.pool, ServerName: "localhost", MinVersion: tls.VersionTLS12}
	return m
}

func (s *smtpServer) only(t *testing.T) received {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.mails) != 1 {
		t.Fatalf("want exactly one mail, got %d", len(s.mails))
	}
	return s.mails[0]
}

func TestSendDeliversOverEveryTransportMode(t *testing.T) {
	for _, mode := range []string{config.SMTPStartTLS, config.SMTPImplicit, config.SMTPPlain} {
		t.Run(mode, func(t *testing.T) {
			srv := newSMTPServer(t, mode)
			m := srv.mailer(mode)
			err := m.Send(context.Background(), Message{To: "member@example.org", Subject: "Hallo Zürich", Body: "Line one\nLine two with ä\n"})
			if err != nil {
				t.Fatal(err)
			}
			got := srv.only(t)
			if got.from != "noreply@example.org" || got.to != "member@example.org" {
				t.Fatalf("envelope: %+v", got)
			}
			assertMessage(t, got.data)
		})
	}
}

func assertMessage(t *testing.T, data string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "Hallo Zürich" {
		t.Fatalf("subject %q: %v", subject, err)
	}
	if msg.Header.Get("Message-ID") == "" || msg.Header.Get("Date") == "" {
		t.Fatal("Message-ID and Date are required")
	}
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if string(body) != "Line one\r\nLine two with ä\r\n" {
		t.Fatalf("body %q", body)
	}
}

func TestSendAuthenticatesWithTheConfiguredAccount(t *testing.T) {
	srv := newSMTPServer(t, config.SMTPStartTLS)
	srv.user, srv.pass = "mailer", "app-password"
	m := srv.mailer(config.SMTPStartTLS)
	if err := m.Send(context.Background(), Message{To: "a@example.org", Subject: "s", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(srv.authSeen) != 1 || srv.authSeen[0] != "|mailer|app-password" {
		t.Fatalf("auth: %v", srv.authSeen)
	}

	m.cfg.Password = "wrong"
	err := m.Send(context.Background(), Message{To: "a@example.org", Subject: "s", Body: "b"})
	var se *SendError
	if !errors.As(err, &se) || se.Stage != "auth" {
		t.Fatalf("wrong password must fail at auth: %v", err)
	}
	if strings.Contains(err.Error(), "wrong") || strings.Contains(err.Error(), "535") {
		t.Fatalf("the error must not carry credentials or server replies: %v", err)
	}
}

func TestSendRefusesToDowngradeStartTLS(t *testing.T) {
	srv := newSMTPServer(t, config.SMTPPlain) // does not offer STARTTLS
	err := srv.mailer(config.SMTPStartTLS).Send(context.Background(), Message{To: "a@example.org", Subject: "s", Body: "b"})
	var se *SendError
	if !errors.As(err, &se) || se.Stage != "tls" {
		t.Fatalf("a missing STARTTLS must stop the delivery: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.mails) != 0 {
		t.Fatal("nothing may be sent in the clear")
	}
}

func TestSendRejectsUntrustedCertificates(t *testing.T) {
	srv := newSMTPServer(t, config.SMTPImplicit)
	m := srv.mailer(config.SMTPImplicit)
	m.tlsConfig = nil // system roots do not know the test certificate
	err := m.Send(context.Background(), Message{To: "a@example.org", Subject: "s", Body: "b"})
	var se *SendError
	if !errors.As(err, &se) || se.Stage != "tls" {
		t.Fatalf("unknown certificate: %v", err)
	}
}

func TestSendRefusesHeaderInjection(t *testing.T) {
	srv := newSMTPServer(t, config.SMTPPlain)
	m := srv.mailer(config.SMTPPlain)
	bad := []Message{
		{To: "a@example.org\r\nBcc: x@example.org", Subject: "s", Body: "b"},
		{To: "a@example.org, b@example.org", Subject: "s", Body: "b"},
		{To: "Name <a@example.org>", Subject: "s", Body: "b"},
		{To: "a@example.org", Subject: "s\r\nBcc: x@example.org", Body: "b"},
		{To: "not an address", Subject: "s", Body: "b"},
	}
	for _, msg := range bad {
		if err := m.Send(context.Background(), msg); err == nil {
			t.Errorf("must refuse %q / %q", msg.To, msg.Subject)
		}
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.mails) != 0 {
		t.Fatalf("nothing may reach the server: %d", len(srv.mails))
	}
}

func TestSendReportsAnUnreachableServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	m, err := New(config.SMTPConfig{Host: "127.0.0.1", Port: port, From: "noreply@example.org", TLS: config.SMTPPlain})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Send(context.Background(), Message{To: "a@example.org", Subject: "s", Body: "b"})
	var se *SendError
	if !errors.As(err, &se) || se.Stage != "connect" {
		t.Fatalf("connect failure expected: %v", err)
	}
}

func TestNewWithoutHostGivesNoMailer(t *testing.T) {
	m, err := New(config.SMTPConfig{})
	if err != nil || m != nil || m.Enabled() {
		t.Fatalf("no host, no mailer: %v %v", m, err)
	}
	if _, err := New(config.SMTPConfig{Host: "h", From: "broken"}); err == nil {
		t.Fatal("a broken sender must be refused")
	}
	srv := newSMTPServer(t, config.SMTPPlain)
	real := srv.mailer(config.SMTPPlain)
	if !real.Enabled() || real.TLS() != config.SMTPPlain || !strings.Contains(real.From(), "noreply@example.org") {
		t.Fatal("accessors")
	}
}
