package setupwizard

import (
	"fmt"
	"io"
	"strings"

	"github.com/msch128/mnema-talk/internal/setupcheck"
)

// startCommand is what starts the stack and waits until it is healthy.
const startCommand = "docker compose up -d --wait"

// step is one numbered item of the closing checklist.
type step struct {
	text string
	// code lines (commands, port table), indented under the text.
	code []string
}

func printSummary(out io.Writer, o Options, g Generated, findings []Finding, container bool) {
	marks := map[setupcheck.Level]string{setupcheck.OK: "[ ok ]", setupcheck.Warn: "[warn]", setupcheck.Fail: "[FAIL]", LevelNote: "[note]"}
	for _, f := range findings {
		fmt.Fprintf(out, "  %s %s: %s\n", marks[f.Level], f.Title, f.Detail)
	}
	fmt.Fprintf(out, "\nWritten:\n  %s   (secrets inside, mode 600)\n  %s\n", g.EnvPath, g.ComposePath)
	fmt.Fprintf(out, "\nOwner account for the first sign-in:\n  user      %s\n  password  %s\n  (also in .env as ADMIN_INITIAL_PASSWORD)\n", o.AdminUsername, g.Secrets.AdminPassword)
	fmt.Fprintln(out, "\nWhat to do next:")
	for i, s := range nextSteps(o, container) {
		fmt.Fprintf(out, "  %d. %s\n", i+1, s.text)
		for _, c := range s.code {
			fmt.Fprintf(out, "       %s\n", c)
		}
	}
	fmt.Fprint(out, "\nUpdate later: docker compose pull && docker compose up -d\n")
}

// nextSteps is the checklist after the files are written: for a trial on this
// computer a short one, for a real address the full way to a working server.
func nextSteps(o Options, container bool) []step {
	url := o.PublicURL
	steps := []step{startStep(container)}
	if o.IsLocal() {
		return append(steps, openStep(url), signInStep(o), passwordStep(),
			step{text: "Try voice: open the page in a second browser tab, sign in with a second account (Admin > Invites) and join the same voice channel. Only this computer can open this address; to invite others later, set PUBLIC_URL=https://chat.example.com in .env (doc/operations.md)."})
	}
	if !strings.HasPrefix(url, "https://") {
		return append(steps, openStep(url), signInStep(o), passwordStep(), plainHTTPStep())
	}
	return append(steps, proxyStep(url, o.AppPort()), openStep(url), signInStep(o), passwordStep(), portsStep(), checkStep())
}

func startStep(container bool) step {
	if container {
		return step{
			text: "Start it on the machine that runs Docker, in the folder you mounted as /out (it holds the two files). This assistant ran inside a container and cannot start it for you:",
			code: []string{"cd <that folder>", startCommand},
		}
	}
	return step{text: "Start it, in the folder with the two files:", code: []string{startCommand}}
}

func proxyStep(url, port string) step {
	return step{
		text: "Put HTTPS in front: set up a reverse proxy (Caddy, nginx, Nginx Proxy Manager, DSM) that forwards " + url + " to http://127.0.0.1:" + port + " with WebSocket support. It carries the web page and chat only, not voice.",
	}
}

func openStep(url string) step { return step{text: "Open " + url + " in your browser."} }

func signInStep(o Options) step {
	return step{text: fmt.Sprintf("Sign in as %q with the password above.", o.AdminUsername)}
}

func passwordStep() step {
	return step{text: "Change the password (your profile settings, Password), then delete the ADMIN_INITIAL_PASSWORD line from .env."}
}

func plainHTTPStep() step {
	return step{text: "This address is plain http://, so it works inside your network only and browsers block the microphone on it from other computers. For members elsewhere use an https:// address and the three port forwards (doc/operations.md)."}
}

func portsStep() step {
	web := "443/tcp"
	return step{
		text: "Forward these three ports from your router to this machine (never 8080) and allow them in its firewall:",
		code: []string{
			portRow(web, "the web page, to the reverse proxy"),
			portRow("50000-50050/udp", "voice and video"),
			portRow("3479/udp and 3479/tcp", "relay for members behind strict firewalls"),
		},
	}
}

func checkStep() step {
	return step{
		text: "Check it from outside: open Admin > Status and press \"Test my connection\", best from a phone on mobile data instead of your Wi-Fi. The same test also reads how many members your internet upload can serve a screen share to (it cannot be measured from inside your network, so no number is shown here). From a shell: docker compose exec app mnema-talk doctor",
	}
}

func portRow(port, what string) string { return fmt.Sprintf("%-22s %s", port, what) }
