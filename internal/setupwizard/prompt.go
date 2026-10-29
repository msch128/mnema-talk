package setupwizard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// errNoInput is returned when a question gets no answer because the input
// ended, e.g. docker run without -it.
var errNoInput = errors.New("no input to read an answer from: run with -it for the questions, or pass the values as flags with --non-interactive")

// prompter asks questions on out and reads the answers from in.
type prompter struct {
	in  *bufio.Reader
	out io.Writer
	// rawIn is the underlying input, used to read a secret without echo.
	rawIn io.Reader
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	return &prompter{in: bufio.NewReader(in), out: out, rawIn: in}
}

// ask prints the question with its default in brackets and returns the
// answer, or the default for an empty line.
func (p *prompter) ask(question, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", question)
	}
	line, err := p.in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || line == "") {
		return "", errNoInput
	}
	if v := strings.TrimSpace(line); v != "" {
		return v, nil
	}
	return def, nil
}

// askYes asks a yes/no question that defaults to no.
func (p *prompter) askYes(question string) (bool, error) {
	a, err := p.ask(question+" (y/N)", "")
	return strings.HasPrefix(strings.ToLower(a), "y"), err
}

// askSecret reads a secret; on a terminal the input is not echoed.
func (p *prompter) askSecret(question string) (string, error) {
	if f, ok := p.rawIn.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(p.out, "%s: ", question)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(p.out)
		return string(b), err
	}
	return p.ask(question, "")
}

// askRequiredURL asks until the answer is a usable public URL.
func (p *prompter) askPublicURL(def string) (string, error) {
	for {
		a, err := p.ask("Public address members will open, e.g. https://chat.example.com", def)
		if err != nil {
			return "", err
		}
		u, err := NormalizePublicURL(a)
		if err == nil {
			return u, nil
		}
		fmt.Fprintln(p.out, "  "+err.Error())
	}
}

// askMail collects the SMTP settings; an empty host skips mail.
func (p *prompter) askMail() (SMTP, error) {
	var s SMTP
	yes, err := p.askYes("Set up outgoing e-mail now? (password reset, address confirmation; can be added later)")
	if err != nil || !yes {
		return s, err
	}
	steps := []struct {
		q    string
		def  string
		into *string
	}{
		{"SMTP server host", "", &s.Host},
		{"SMTP port", "587", &s.Port},
		{"Encryption (starttls, tls or plain)", "starttls", &s.TLS},
		{"SMTP user name (empty for none)", "", &s.Username},
		{"Sender address, e.g. Mnema Talk <noreply@example.org>", "", &s.From},
	}
	for _, st := range steps {
		if *st.into, err = p.ask(st.q, st.def); err != nil {
			return s, err
		}
	}
	if s.Username != "" {
		s.Password, err = p.askSecret("SMTP password")
	}
	return s, err
}
