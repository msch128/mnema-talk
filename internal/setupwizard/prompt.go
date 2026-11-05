package setupwizard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/msch128/mnema-talk/internal/auth"
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

// explain prints a short paragraph under the numbered heading of a question:
// what it is, why it matters, and an example.
func (p *prompter) explain(heading string, lines ...string) {
	fmt.Fprintf(p.out, "\n%s\n", heading)
	for _, l := range lines {
		fmt.Fprintf(p.out, "    %s\n", l)
	}
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

// askPublicURL asks until the answer is a usable public URL.
func (p *prompter) askPublicURL(def string) (string, error) {
	for {
		a, err := p.ask("Public address", def)
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

// askAdminUsername asks until the answer is a name the server can seed.
func (p *prompter) askAdminUsername(def string) (string, error) {
	for {
		a, err := p.ask("User name of the Owner account", def)
		if err != nil {
			return "", err
		}
		if err := auth.ValidateAdminSeed(a, ""); err == nil {
			return a, nil
		} else {
			fmt.Fprintln(p.out, "  "+err.Error())
		}
	}
}

// askMail collects the SMTP settings; an empty host skips mail.
func (p *prompter) askMail() (SMTP, error) {
	var s SMTP
	yes, err := p.askYes("Set up outgoing e-mail now?")
	if err != nil || !yes {
		return s, err
	}
	if s.Host, err = p.ask("SMTP server host (empty to skip mail)", ""); err != nil || s.Host == "" {
		return SMTP{}, err
	}
	if err = p.askMailDetails(&s); err != nil {
		return s, err
	}
	if s.Username != "" {
		s.Password, err = p.askSecret("SMTP password")
	}
	return s, err
}

// askMailDetails asks for the rest of the mail setup after its host; the
// encryption and the sender are asked again until the server would accept them.
func (p *prompter) askMailDetails(s *SMTP) (err error) {
	if s.Port, err = p.ask("SMTP port", "587"); err != nil {
		return err
	}
	for {
		if s.TLS, err = p.ask("Encryption (starttls, tls or plain)", "starttls"); err != nil {
			return err
		}
		if err = validateSMTPTLS(s.TLS); err == nil {
			break
		}
		fmt.Fprintln(p.out, "  "+err.Error())
	}
	if s.Username, err = p.ask("SMTP user name (empty for none)", ""); err != nil {
		return err
	}
	for s.From == "" {
		if s.From, err = p.ask("Sender address, e.g. Mnema Talk <noreply@example.org>", ""); err != nil {
			return err
		}
		if s.From == "" {
			fmt.Fprintln(p.out, "  mail needs a sender address")
		}
	}
	return nil
}
