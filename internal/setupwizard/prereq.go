package setupwizard

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/msch128/mnema-talk/internal/portconflict"
	"github.com/msch128/mnema-talk/internal/setupcheck"
)

// Ports the stack needs on the host, with the setting that moves each.
func (o Options) hostPorts() []hostPort {
	return []hostPort{
		{"tcp", o.AppPort(), "PORT"},
		{"tcp", "3479", "WEBRTC_BUILTIN_TURN_PORT"},
		{"udp", "3479", "WEBRTC_BUILTIN_TURN_PORT"},
		{"udp", "50000", "WEBRTC_UDP_PORT_MIN and WEBRTC_UDP_PORT_MAX"},
	}
}

type hostPort struct{ proto, port, env string }

// machineFindings checks what can be seen of the machine: Docker Compose and
// free ports. Inside the docker run --rm container neither is visible, which
// is said instead of guessed.
func machineFindings(ctx context.Context, o Options, p Probe) []Finding {
	var host setupcheck.Host
	if p.Host != nil {
		host = p.Host()
	}
	if host.InContainer {
		return []Finding{{LevelNote, "Docker and ports", "The assistant runs inside a container, so it cannot start the stack or look at this machine's ports. The checklist below has the exact commands to run on the machine; `docker compose up` reports a port that is taken."}}
	}
	var out []Finding
	if p.Compose != nil {
		out = append(out, composeFinding(p.Compose(ctx)))
	}
	if p.PortFree != nil {
		out = append(out, portsFinding(o, p.PortFree))
	}
	return out
}

func composeFinding(err error) Finding {
	if err != nil {
		return Finding{setupcheck.Warn, "Docker Compose", "`docker compose` does not work here (" + err.Error() + "). Install Docker with the Compose plugin: https://docs.docker.com/engine/install/"}
	}
	return Finding{setupcheck.OK, "Docker Compose", "`docker compose` is available."}
}

func portsFinding(o Options, free func(proto string, port int) bool) Finding {
	var busy []string
	for _, hp := range o.hostPorts() {
		n, _ := strconv.Atoi(hp.port)
		if !free(hp.proto, n) {
			busy = append(busy, portconflict.Hint(hp.port, strings.ToUpper(hp.proto), hp.env))
		}
	}
	if len(busy) > 0 {
		return Finding{setupcheck.Warn, "Ports", strings.Join(busy, ". ") + "."}
	}
	return Finding{setupcheck.OK, "Ports", fmt.Sprintf("TCP %s, TCP and UDP 3479 and UDP 50000 are free on this machine.", o.AppPort())}
}

// networkFinding turns the facts about the connection into the CGNAT / no
// IPv4 verdict of the server's own check, with the same texts.
func networkFinding(ctx context.Context, public netip.Addr, pubErr error, p Probe) (Finding, bool) {
	f := setupcheck.NetworkFacts{STUN: public, STUNFailed: pubErr != nil}
	if p.Host != nil {
		f.Local = p.Host().Local
	}
	if f.STUNFailed && p.PublicIP6 != nil {
		_, err := p.PublicIP6(ctx)
		f.IPv6OK = err == nil
	}
	code, level, params := setupcheck.ClassifyNetwork(f)
	if code == "" {
		return Finding{}, false
	}
	c := setupcheck.NetworkCheck(code, level, params)
	return Finding{c.Level, "Home connection", c.Detail + " " + c.Fix}, true
}

// portFree binds the port briefly; a port that is taken says so, any other
// error (a privileged port, say) does not count as taken.
func portFree(proto string, port int) bool {
	addr := net.JoinHostPort("", strconv.Itoa(port))
	if proto == "udp" {
		pc, err := net.ListenPacket("udp4", addr)
		if err == nil {
			_ = pc.Close()
		}
		return !portconflict.InUse(err)
	}
	l, err := net.Listen("tcp4", addr)
	if err == nil {
		_ = l.Close()
	}
	return !portconflict.InUse(err)
}

// composeAvailable runs `docker compose version`.
func composeAvailable(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "compose", "version").CombinedOutput(); err != nil {
		if _, lookErr := exec.LookPath("docker"); lookErr != nil {
			return fmt.Errorf("docker not found")
		}
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}
