package setupwizard

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/msch128/mnema-talk/internal/setupcheck"
)

// Env is what Run needs from the outside, replaceable in tests.
type Env struct {
	In    io.Reader
	Out   io.Writer
	Probe Probe
	Now   func() time.Time
}

const usage = `usage: mnema-talk setup [flags]

Writes .env and docker-compose.yml for a new installation. Without
--non-interactive it asks for what the flags leave out.

  --dir DIR               where to write the files (default /out in the image, else .)
  --public-url URL        the address members open, e.g. https://chat.example.com
  --language en|de        language of the default channel names (default en)
  --admin-username NAME   the Owner's user name
  --smtp-host HOST        outgoing mail (with --smtp-port, --smtp-tls, --smtp-username,
  --smtp-password, --smtp-from)
  --auto-update           enable the "Update now" button (updater sidecar)
  --image REF             image to run (default: this build's major.minor tag)
  --compose-template FILE docker-compose.yml to derive from
  --skip-preflight        do not touch the network (DNS, STUN, NTP)
  --force                 replace existing files (generates new secrets!)
  --non-interactive, -y   never ask; --public-url is then required`

// parseFlags reads the command line into Options.
func parseFlags(args []string) (o Options, nonInteractive bool, err error) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Dir, "dir", "", "")
	fs.StringVar(&o.PublicURL, "public-url", "", "")
	fs.StringVar(&o.Language, "language", "", "")
	fs.StringVar(&o.AdminUsername, "admin-username", "", "")
	fs.StringVar(&o.SMTP.Host, "smtp-host", "", "")
	fs.StringVar(&o.SMTP.Port, "smtp-port", "", "")
	fs.StringVar(&o.SMTP.TLS, "smtp-tls", "", "")
	fs.StringVar(&o.SMTP.Username, "smtp-username", "", "")
	fs.StringVar(&o.SMTP.Password, "smtp-password", "", "")
	fs.StringVar(&o.SMTP.From, "smtp-from", "", "")
	fs.BoolVar(&o.AutoUpdate, "auto-update", false, "")
	fs.StringVar(&o.Image, "image", "", "")
	fs.StringVar(&o.TemplatePath, "compose-template", "", "")
	fs.BoolVar(&o.SkipPreflight, "skip-preflight", false, "")
	fs.BoolVar(&o.Force, "force", false, "")
	fs.BoolVar(&nonInteractive, "non-interactive", false, "")
	fs.BoolVar(&nonInteractive, "y", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, false, errors.New(usage)
		}
		return o, false, fmt.Errorf("%w\n%s", err, usage)
	}
	if fs.NArg() > 0 {
		return o, false, fmt.Errorf("unexpected argument %q\n%s", fs.Arg(0), usage)
	}
	return o, nonInteractive, nil
}

// Run is `mnema-talk setup`.
func Run(ctx context.Context, args []string, env Env) error {
	o, nonInteractive, err := parseFlags(args)
	if err != nil {
		return err
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	if o.Dir == "" {
		o.Dir = defaultDir()
	}
	if nonInteractive {
		if o.PublicURL == "" {
			return errors.New("--public-url is required with --non-interactive")
		}
	} else if o, err = askMissing(newPrompter(env.In, env.Out), o); err != nil {
		return err
	}
	if o, err = o.normalize(); err != nil {
		return err
	}
	template, err := readTemplate(o.TemplatePath)
	if err != nil {
		return err
	}
	var findings []Finding
	if !o.SkipPreflight {
		fmt.Fprintln(env.Out, "\nChecking what can be checked from here ...")
		findings = Preflight(ctx, o, env.Probe)
	}
	g, err := Write(o, template, NewSecrets(), env.Now())
	if err != nil {
		return err
	}
	printSummary(env.Out, o, g, findings)
	return nil
}

// askMissing asks for every value the flags did not give.
func askMissing(p *prompter, o Options) (Options, error) {
	var err error
	fmt.Fprintln(p.out, "Mnema Talk setup. Press Enter to accept the value in brackets.")
	if o.PublicURL == "" {
		if o.PublicURL, err = p.askPublicURL(""); err != nil {
			return o, err
		}
	}
	if o.AdminUsername == "" {
		if o.AdminUsername, err = p.ask("User name of the Owner account", DefaultAdminUsername); err != nil {
			return o, err
		}
	}
	if o.Language == "" {
		if o.Language, err = p.ask("Language of the default channel names (en or de)", "en"); err != nil {
			return o, err
		}
	}
	if !o.SMTP.Enabled() {
		if o.SMTP, err = p.askMail(); err != nil {
			return o, err
		}
	}
	if !o.AutoUpdate {
		fmt.Fprintln(p.out, "The updater sidecar adds an \"Update now\" button, but it holds the Docker socket (see SECURITY.md).")
		o.AutoUpdate, err = p.askYes("Enable the updater sidecar?")
	}
	return o, err
}

func printSummary(out io.Writer, o Options, g Generated, findings []Finding) {
	marks := map[setupcheck.Level]string{setupcheck.OK: "[ ok ]", setupcheck.Warn: "[warn]", setupcheck.Fail: "[FAIL]", LevelNote: "[note]"}
	for _, f := range findings {
		fmt.Fprintf(out, "  %s %s: %s\n", marks[f.Level], f.Title, f.Detail)
	}
	fmt.Fprintf(out, "\nWritten:\n  %s   (secrets inside, mode 600)\n  %s\n", g.EnvPath, g.ComposePath)
	fmt.Fprintf(out, "\nOwner sign-in after the first start:\n  user      %s\n  password  %s\n  (also in .env as ADMIN_INITIAL_PASSWORD; delete that line after signing in)\n", o.AdminUsername, g.Secrets.AdminPassword)
	fmt.Fprint(out, "\nNext, in the directory with the two files:\n  docker compose up -d --wait\n")
	if strings.HasPrefix(o.PublicURL, "https://") {
		fmt.Fprintf(out, "\nPut a reverse proxy with TLS in front: %s -> http://127.0.0.1:8080 (WebSocket enabled).\n", o.PublicURL)
	}
	fmt.Fprint(out, "Forward UDP 50000-50050 to this host for voice. Open the admin console, Status tab: the setup checks name anything still missing.\n")
	fmt.Fprint(out, "Update later: docker compose pull && docker compose up -d\n")
}

// defaultDir is /out when the image is run with a mount there (the documented
// docker run command), else the current directory.
func defaultDir() string {
	if fi, err := os.Stat("/out"); err == nil && fi.IsDir() {
		return "/out"
	}
	return "."
}
