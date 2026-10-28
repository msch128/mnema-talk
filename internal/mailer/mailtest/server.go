//go:build integration

// Package mailtest is a throw-away SMTP server for integration tests: it
// accepts plain connections on a loopback port and keeps what it received.
package mailtest

import (
	"bufio"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
)

// Mail is one received message.
type Mail struct {
	To      string
	Subject string
	Body    string
}

// Server is the fake SMTP server.
type Server struct {
	t    testing.TB
	ln   net.Listener
	mu   sync.Mutex
	mail []Mail
}

// Start listens on a free loopback port until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{t: t, ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

// Config is the SMTP setting that points the server at s.
func (s *Server) Config() config.SMTPConfig {
	port := s.ln.Addr().(*net.TCPAddr).Port
	return config.SMTPConfig{Host: "127.0.0.1", Port: port, From: "Mnema Talk <noreply@example.org>", TLS: config.SMTPPlain}
}

// Messages returns what arrived so far.
func (s *Server) Messages() []Mail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mail(nil), s.mail...)
}

// WaitFor blocks until n messages arrived (and fails the test after a while).
func (s *Server) WaitFor(t testing.TB, n int) []Mail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.Messages(); len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d mails, got %d", n, len(s.Messages()))
	return nil
}

var linkPattern = regexp.MustCompile(`https?://\S+`)

// Link is the first URL in the body; the token is in its fragment.
func (m Mail) Link() string { return linkPattern.FindString(m.Body) }

// Token is the value after "#token=" in the link.
func (m Mail) Token() string {
	_, tok, _ := strings.Cut(m.Link(), "#token=")
	return tok
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	say := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	say("220 mailtest")
	var to string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch verb := strings.ToUpper(strings.Fields(line + " ")[0]); verb {
		case "EHLO", "HELO":
			say("250 mailtest")
		case "RCPT":
			to = strings.Trim(line[strings.Index(line, ":")+1:], " <>\r\n")
			say("250 ok")
		case "DATA":
			say("354 go")
			s.store(to, readData(r))
			say("250 queued")
		case "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func readData(r *bufio.Reader) string {
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil || line == ".\r\n" {
			return b.String()
		}
		// Undo the dot-stuffing of the sender (RFC 5321, section 4.5.2).
		b.WriteString(strings.TrimPrefix(line, "."))
	}
}

func (s *Server) store(to, raw string) {
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		s.t.Errorf("mailtest: unparseable message for %s: %v\n%s", to, err, raw)
		return
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	body, _ := io.ReadAll(quotedprintable.NewReader(msg.Body))
	s.mu.Lock()
	s.mail = append(s.mail, Mail{To: to, Subject: subject, Body: strings.ReplaceAll(string(body), "\r\n", "\n")})
	s.mu.Unlock()
}
