package setupwizard

import (
	"fmt"

	"github.com/msch128/mnema-talk/internal/setupcheck"
)

// Each question says in plain words what it is, why it matters and gives an
// example before it asks (explain), so nobody has to know the vocabulary.

// askMissing asks for every value the flags did not give. lanHint is the
// machine's own LAN address when the wizard runs on the host and can see it.
func askMissing(p *prompter, o Options, lanHint string) (Options, error) {
	fmt.Fprintln(p.out, "Mnema Talk setup. Press Enter to accept the value in brackets.")
	steps := []func(*prompter, Options, string) (Options, error){askWhere, askOwner, askLanguage, askLAN, askMailStep, askUpdater}
	var err error
	for _, step := range steps {
		if o, err = step(p, o, lanHint); err != nil {
			return o, err
		}
	}
	return o, nil
}

// askWhere decides between a trial on this computer and a real address.
func askWhere(p *prompter, o Options, _ string) (Options, error) {
	var err error
	if o.PublicURL == "" && !o.Local {
		p.explain("Where will you use it?",
			"The quickest way to see Mnema Talk working is on this computer alone: no domain,",
			"no certificate, no router settings. Only this computer can open it, so nobody",
			"else can join. Say no if other people should use it.")
		if o.Local, err = p.askYes("Just try it on this computer?"); err != nil {
			return o, err
		}
	}
	if o.PublicURL != "" || o.Local {
		return o, nil
	}
	p.explain("Public address",
		"The address members type into their browser. It becomes PUBLIC_URL: invite links,",
		"e-mails and the browser's origin checks use it, so it must match exactly. You need",
		"a domain name whose DNS record points to this network or server.",
		"Example: https://chat.example.com")
	o.PublicURL, err = p.askPublicURL("")
	return o, err
}

func askOwner(p *prompter, o Options, _ string) (Options, error) {
	if o.AdminUsername != "" {
		return o, nil
	}
	p.explain("Owner account",
		"The first account, which manages the server (members, channels, settings). Everyone",
		"else gets their own account later through an invite. The setup assistant generates",
		"its password and shows it at the end.",
		"Example: admin")
	var err error
	o.AdminUsername, err = p.askAdminUsername(DefaultAdminUsername)
	return o, err
}

func askLanguage(p *prompter, o Options, _ string) (Options, error) {
	if o.Language != "" {
		return o, nil
	}
	p.explain("Language of the default channels",
		"A new installation starts with a few ready-made categories and channels (Text channels,",
		"#general, Lounge ...). This is the language of their names, used once at the first",
		"start. Members choose their own interface language in their settings.",
		"Answer: de or en")
	var err error
	o.Language, err = p.ask("Language of the default channel names (en or de)", DefaultLanguage)
	return o, err
}

// askLAN asks for the host's LAN address. A trial on this computer, or an
// address given by flag, needs no question.
func askLAN(p *prompter, o Options, hint string) (Options, error) {
	if o.LANIP != "" || o.Local || (o.PublicURL != "" && o.IsLocal()) {
		return o, nil
	}
	p.explain("Address of this machine in your own network (optional)",
		"Members at home reach voice through it even when your router cannot loop back to",
		"its own public address (a common router limit, called NAT loopback or hairpin).",
		"Find it in your router's device list, or with `ip addr` on the host. Leave it empty",
		"if you do not know it or the server runs at a hosting provider.",
		"Example: 192.168.1.20")
	for {
		v, err := p.ask("LAN address of this machine", hint)
		if err != nil {
			return o, err
		}
		if err := validateLANIP(v); err != nil {
			fmt.Fprintln(p.out, "  "+err.Error())
			continue
		}
		o.LANIP = v
		return o, nil
	}
}

func askMailStep(p *prompter, o Options, _ string) (Options, error) {
	if o.SMTP.Enabled() {
		return o, nil
	}
	p.explain("Outgoing e-mail (optional)",
		"With a mail server Mnema Talk can send password reset links and confirm e-mail",
		"addresses. Without one everything else works; you can add it later in .env.",
		"You need the host name of a mail server (SMTP) and a sender address.")
	var err error
	o.SMTP, err = p.askMail()
	return o, err
}

func askUpdater(p *prompter, o Options, _ string) (Options, error) {
	if o.AutoUpdate {
		return o, nil
	}
	p.explain("One-click updates (optional)",
		"The updater sidecar adds an \"Update now\" button to the admin console. In return it",
		"holds the Docker socket, which is powerful (see SECURITY.md). Without it you update",
		"with two commands: docker compose pull && docker compose up -d")
	var err error
	o.AutoUpdate, err = p.askYes("Enable the updater sidecar?")
	return o, err
}

// lanSuggestion is the machine's own private address when it is worth
// offering as the default answer: only a process outside a container sees the
// host's real LAN address.
func lanSuggestion(h setupcheck.Host) string {
	if h.InContainer || !h.Local.IsValid() || !h.Local.Is4() || !h.Local.IsPrivate() {
		return ""
	}
	return h.Local.String()
}
