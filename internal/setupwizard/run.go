package setupwizard

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/msch128/mnema-talk/internal/setupcheck"
)

// Env is what Run needs from the outside, replaceable in tests.
type Env struct {
	In    io.Reader
	Out   io.Writer
	Probe Probe
	Now   func() time.Time
	// Exec runs docker compose for --start; nil runs the real command.
	Exec Exec
}

const usage = `usage: mnema-talk setup [flags]

Writes .env and docker-compose.yml for a new installation, checks what it can
see (Docker Compose, free ports, DNS, a home connection without its own public
IPv4 address) and ends with a numbered list of what to do next. Without
--non-interactive it explains each question and asks for what the flags leave
out. Afterwards, "mnema-talk doctor" repeats the checks against the running
server.

  --dir DIR               where to write the files (default /out in the image, else .)
  --public-url URL        the address members open, e.g. https://chat.example.com
  --local                 just try it on this computer: PUBLIC_URL=http://localhost:8080,
                          nothing reachable from outside (no domain, certificate or router)
  --language en|de        language of the default channel names (default de, as INITIAL_LANGUAGE)
  --lan-ip IP             this machine's address in your network (WEBRTC_LAN_IP), so members
                          at home reach voice without NAT loopback
  --admin-username NAME   the Owner's user name
  --smtp-host HOST        outgoing mail (with --smtp-port, --smtp-tls, --smtp-username,
  --smtp-password, --smtp-from)
  --auto-update           enable the "Update now" button (updater sidecar)
  --image REF             image to run (default: this build's major.minor tag)
  --compose-template FILE docker-compose.yml to derive from
  --start                 after writing: docker compose up -d --wait, then the live checks
                          (the same as the admin console's Status tab). Needs Docker Compose
                          on this machine; in the docker run --rm container it prints the command
  --skip-preflight        do not touch the network (DNS, STUN, NTP)
  --check                 only check the .env in --dir for missing or invalid required
                          settings (what the server refuses to start without), then exit
  --force                 replace existing files (generates new secrets!)
  --non-interactive, -y   never ask; --public-url (or --local) is then required`

// parseFlags reads the command line into Options.
func parseFlags(args []string) (o Options, nonInteractive bool, err error) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Dir, "dir", "", "")
	fs.StringVar(&o.PublicURL, "public-url", "", "")
	fs.StringVar(&o.Language, "language", "", "")
	fs.BoolVar(&o.Local, "local", false, "")
	fs.StringVar(&o.LANIP, "lan-ip", "", "")
	fs.BoolVar(&o.Start, "start", false, "")
	fs.BoolVar(&o.CheckOnly, "check", false, "")
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
	host := hostOf(env.Probe)
	pr := newPrompter(env.In, env.Out)
	if done, err := checkExistingEnv(env.Out, o); done {
		return err
	}
	if nonInteractive {
		if o.PublicURL == "" && !o.Local {
			return errors.New("--public-url (or --local) is required with --non-interactive")
		}
	} else if o, err = askMissing(pr, o, lanSuggestion(host)); err != nil {
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
	printSummary(env.Out, o, g, findings, host.InContainer)
	offerStart(ctx, pr, o, g.Dir(), env, !nonInteractive)
	return nil
}

// checkExistingEnv looks at an .env that is already in the directory. With
// --check that is the whole job; otherwise it explains what is wrong with it
// before refusing to replace it (done is then true).
func checkExistingEnv(out io.Writer, o Options) (done bool, err error) {
	if !o.CheckOnly && o.Force {
		return false, nil
	}
	found, problems, err := checkExisting(out, o.Dir)
	switch {
	case err != nil:
		return true, err
	case o.CheckOnly && !found:
		return true, fmt.Errorf("no .env in %s to check", o.Dir)
	case o.CheckOnly && len(problems) > 0:
		return true, errors.New("fix the settings above and run the check again")
	case o.CheckOnly:
		return true, nil
	case found:
		return true, fmt.Errorf("%s already exists and was left alone; to start over (this generates new secrets, which an existing database will not accept) remove it or pass --force", filepath.Join(o.Dir, ".env"))
	}
	return false, nil
}

func hostOf(p Probe) setupcheck.Host {
	if p.Host == nil {
		return setupcheck.Host{}
	}
	return p.Host()
}

// defaultDir is /out when the image is run with a mount there (the documented
// docker run command), else the current directory.
func defaultDir() string {
	if fi, err := os.Stat("/out"); err == nil && fi.IsDir() {
		return "/out"
	}
	return "."
}
