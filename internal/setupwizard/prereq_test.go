package setupwizard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/setupcheck"
)

func findingFor(fs []Finding, title string) (Finding, bool) {
	for _, f := range fs {
		if f.Title == title {
			return f, true
		}
	}
	return Finding{}, false
}

func hostProbe(h setupcheck.Host) Probe {
	p := fakeProbe()
	p.Host = func() setupcheck.Host { return h }
	p.PortFree = func(string, int) bool { return true }
	p.Compose = func(context.Context) error { return nil }
	return p
}

func TestPreflightChecksDockerAndPortsOnTheHost(t *testing.T) {
	o, _ := Options{PublicURL: "https://chat.example.com"}.normalize()
	fs := Preflight(context.Background(), o, hostProbe(setupcheck.Host{Local: netip.MustParseAddr("192.168.1.20")}))
	if f, ok := findingFor(fs, "Docker Compose"); !ok || f.Level != setupcheck.OK {
		t.Errorf("Docker Compose: %+v", f)
	}
	if f, ok := findingFor(fs, "Ports"); !ok || f.Level != setupcheck.OK {
		t.Errorf("Ports: %+v", f)
	}
}

func TestPreflightNamesTheBusyPortAndHowToMoveIt(t *testing.T) {
	o, _ := Options{PublicURL: "https://chat.example.com"}.normalize()
	p := hostProbe(setupcheck.Host{})
	p.PortFree = func(proto string, port int) bool {
		return !(proto == "tcp" && port == 8080) && !(proto == "udp" && port == 3479)
	}
	f, _ := findingFor(Preflight(context.Background(), o, p), "Ports")
	if f.Level != setupcheck.Warn {
		t.Fatalf("level = %s", f.Level)
	}
	for _, want := range []string{"TCP port 8080", "PORT in .env", "UDP port 3479", "WEBRTC_BUILTIN_TURN_PORT", "another program"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("detail lacks %q: %s", want, f.Detail)
		}
	}
	if strings.Contains(f.Detail, "TCP port 3479") {
		t.Errorf("a free port is reported: %s", f.Detail)
	}
}

func TestPreflightWithoutComposeSaysHowToGetIt(t *testing.T) {
	o, _ := Options{PublicURL: "https://chat.example.com"}.normalize()
	p := hostProbe(setupcheck.Host{})
	p.Compose = func(context.Context) error { return errors.New("docker not found") }
	f, _ := findingFor(Preflight(context.Background(), o, p), "Docker Compose")
	if f.Level != setupcheck.Warn || !strings.Contains(f.Detail, "docs.docker.com") {
		t.Fatalf("%+v", f)
	}
}

func TestPreflightInTheRunContainerLooksAtNeitherDockerNorPorts(t *testing.T) {
	o, _ := Options{PublicURL: "https://chat.example.com"}.normalize()
	p := hostProbe(setupcheck.Host{InContainer: true})
	p.Compose = func(context.Context) error { t.Fatal("asked for Docker from inside the container"); return nil }
	p.PortFree = func(string, int) bool { t.Fatal("tested ports from inside the container"); return false }
	fs := Preflight(context.Background(), o, p)
	f, ok := findingFor(fs, "Docker and ports")
	if !ok || f.Level != LevelNote {
		t.Fatalf("%+v", fs)
	}
}

func TestPreflightFlagsAHomeConnectionWithoutPublicIPv4(t *testing.T) {
	o, _ := Options{PublicURL: "https://chat.example.com"}.normalize()
	cgnat := hostProbe(setupcheck.Host{Local: netip.MustParseAddr("100.64.3.2")})
	cgnat.PublicIP = func(context.Context) (netip.Addr, error) { return netip.Addr{}, errors.New("no answer") }
	f, ok := findingFor(Preflight(context.Background(), o, cgnat), "Home connection")
	if !ok || f.Level != setupcheck.Warn || !strings.Contains(f.Detail, "port forwarding cannot work") || !strings.Contains(f.Detail, "VPS") {
		t.Errorf("CGNAT: %+v", f)
	}

	dsLite := hostProbe(setupcheck.Host{})
	dsLite.PublicIP = func(context.Context) (netip.Addr, error) { return netip.Addr{}, errors.New("no answer") }
	dsLite.PublicIP6 = func(context.Context) (netip.Addr, error) { return netip.MustParseAddr("2001:db8::7"), nil }
	if f, ok := findingFor(Preflight(context.Background(), o, dsLite), "Home connection"); !ok || !strings.Contains(f.Detail, "IPv6 works") {
		t.Errorf("DS-Lite: %+v", f)
	}

	if _, ok := findingFor(Preflight(context.Background(), o, hostProbe(setupcheck.Host{})), "Home connection"); ok {
		t.Error("a healthy connection got a verdict")
	}
}

func TestPreflightForATrialSkipsTheNetworkWork(t *testing.T) {
	o, _ := Options{Local: true}.normalize()
	p := hostProbe(setupcheck.Host{})
	p.PublicIP = func(context.Context) (netip.Addr, error) {
		t.Fatal("STUN asked for a local trial")
		return netip.Addr{}, nil
	}
	p.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		t.Fatal("DNS asked for a local trial")
		return nil, nil
	}
	fs := Preflight(context.Background(), o, p)
	for _, f := range fs {
		if f.Title == "DNS" || f.Title == "Public address" {
			t.Errorf("unexpected %+v", f)
		}
	}
}

func TestRealPortFreeSeesATakenPort(t *testing.T) {
	l, err := net.Listen("tcp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	if portFree("tcp", port) {
		t.Errorf("tcp %d is held but reported free", port)
	}
	pc, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if portFree("udp", pc.LocalAddr().(*net.UDPAddr).Port) {
		t.Error("a held udp port is reported free")
	}
}

type recordedExec struct {
	calls []string
	fail  map[string]error
}

func (r *recordedExec) run(_ context.Context, dir string, out io.Writer, name string, args ...string) error {
	line := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, line)
	fmt.Fprintln(out, "(ran "+line+")")
	for k, err := range r.fail {
		if strings.Contains(line, k) {
			return err
		}
	}
	return nil
}

func runWithHost(t *testing.T, host setupcheck.Host, input string, ex *recordedExec, args ...string) string {
	t.Helper()
	p := hostProbe(host)
	var out strings.Builder
	err := Run(context.Background(), append([]string{"--dir", t.TempDir(), "--compose-template", repoCompose}, args...), Env{In: strings.NewReader(input), Out: &out, Probe: p, Now: func() time.Time { return fixedNow }, Exec: ex.run})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	return out.String()
}

func TestStartRunsComposeThenTheLiveChecks(t *testing.T) {
	ex := &recordedExec{}
	out := runWithHost(t, setupcheck.Host{}, "", ex, "-y", "--start", "--public-url", "https://chat.example.com")
	if len(ex.calls) != 2 || !strings.HasPrefix(ex.calls[0], "docker compose up -d --wait") || ex.calls[1] != "docker compose exec -T app mnema-talk doctor" {
		t.Fatalf("calls = %q", ex.calls)
	}
	if strings.Contains(out, "did not become healthy") {
		t.Error(out)
	}
}

func TestStartReportsAStackThatDoesNotComeUp(t *testing.T) {
	ex := &recordedExec{fail: map[string]error{"up -d": errors.New("exit status 1")}}
	out := runWithHost(t, setupcheck.Host{}, "", ex, "-y", "--start", "--public-url", "https://chat.example.com")
	if len(ex.calls) != 1 || !strings.Contains(out, "docker compose logs app") {
		t.Fatalf("calls %q\n%s", ex.calls, out)
	}
}

func TestStartIsOptInAndAsksOnlyWhenInteractive(t *testing.T) {
	ex := &recordedExec{}
	runWithHost(t, setupcheck.Host{}, "", ex, "-y", "--public-url", "https://chat.example.com")
	if len(ex.calls) != 0 {
		t.Fatalf("started without being asked: %q", ex.calls)
	}
	answers := "n\nhttps://chat.example.com\n\n\n\n\n\ny\n" // owner, language, LAN, mail, updater by default; start yes
	runWithHost(t, setupcheck.Host{}, answers, ex)
	if len(ex.calls) != 2 {
		t.Fatalf("accepting the question did not start the stack: %q", ex.calls)
	}
}

func TestInTheRunContainerStartPrintsTheCommandInstead(t *testing.T) {
	ex := &recordedExec{}
	out := runWithHost(t, setupcheck.Host{InContainer: true}, "", ex, "-y", "--start", "--public-url", "https://chat.example.com")
	if len(ex.calls) != 0 {
		t.Fatalf("ran Docker from inside the container: %q", ex.calls)
	}
	for _, want := range []string{"cannot start it for you", "docker compose up -d --wait", "--start needs Docker Compose"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestLocalTrialWorksOutOfTheBox(t *testing.T) {
	dir := t.TempDir()
	out, err := runSetup(t, "", "-y", "--local", "--dir", dir, "--compose-template", repoCompose)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".env"))
	env, _ := godotenv.Unmarshal(string(raw))
	if env["PUBLIC_URL"] != "http://localhost:8080" || env["PUBLISH_ADDR"] != "127.0.0.1" {
		t.Errorf("PUBLIC_URL=%q PUBLISH_ADDR=%q", env["PUBLIC_URL"], env["PUBLISH_ADDR"])
	}
	cfg := asProductionConfig(t, string(raw))
	if !cfg.PublicURLIsLocal() {
		t.Error("not a local config")
	}
	if !strings.Contains(out, "Open http://localhost:8080") {
		t.Errorf("summary:\n%s", out)
	}
}

func TestLocalTrialOnAnotherPort(t *testing.T) {
	dir := t.TempDir()
	if _, err := runSetup(t, "", "-y", "--dir", dir, "--public-url", "http://localhost:9000", "--compose-template", repoCompose); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".env"))
	env, _ := godotenv.Unmarshal(string(raw))
	if env["PORT"] != "9000" || env["PUBLISH_ADDR"] != "127.0.0.1" {
		t.Errorf("PORT=%q PUBLISH_ADDR=%q", env["PORT"], env["PUBLISH_ADDR"])
	}
}

func TestInteractiveSetupOffersTheLocalTrial(t *testing.T) {
	dir := t.TempDir()
	out, err := runSetup(t, "y\n\n\n\n\n", "--dir", dir, "--compose-template", repoCompose, "--skip-preflight")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(raw), "PUBLIC_URL=http://localhost:8080") {
		t.Errorf(".env:\n%s", raw)
	}
	if strings.Contains(out, "Public address") {
		t.Error("the trial still asked for an address")
	}
}

func TestLANAddressIsAskedValidatedAndWritten(t *testing.T) {
	dir := t.TempDir()
	answers := "n\nhttps://chat.example.com\n\n\nnot an ip\n192.168.1.20\n\n\n"
	out, err := runSetup(t, answers, "--dir", dir, "--compose-template", repoCompose, "--skip-preflight")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "is not the IPv4 address of this machine") {
		t.Errorf("a bad address got no explanation:\n%s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".env"))
	cfg := asProductionConfig(t, string(raw))
	if strings.Join(cfg.WebRTCLANIPs, ",") != "192.168.1.20" {
		t.Errorf("WEBRTC_LAN_IP = %v", cfg.WebRTCLANIPs)
	}
}

func TestLANAddressFlagRejectsNonsense(t *testing.T) {
	for _, v := range []string{"example.org", "127.0.0.1", "::1", "0.0.0.0"} {
		if _, err := runSetup(t, "", "-y", "--dir", t.TempDir(), "--public-url", "https://a.example.com", "--lan-ip", v, "--compose-template", repoCompose); err == nil || !strings.Contains(err.Error(), "--lan-ip") {
			t.Errorf("%q: err = %v", v, err)
		}
	}
}

func TestOnTheHostTheLANAddressIsOfferedAsDefault(t *testing.T) {
	dir := t.TempDir()
	p := hostProbe(setupcheck.Host{Local: netip.MustParseAddr("192.168.1.77")})
	var out strings.Builder
	answers := "n\nhttps://chat.example.com\n\n\n\n\n\n"
	if err := Run(context.Background(), []string{"--dir", dir, "--compose-template", repoCompose, "--skip-preflight"}, Env{In: strings.NewReader(answers), Out: &out, Probe: p}); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".env"))
	if !strings.Contains(string(raw), "WEBRTC_LAN_IP=192.168.1.77") {
		t.Errorf(".env lacks the offered LAN address:\n%s", raw)
	}
}

// One documented default for INITIAL_LANGUAGE: the server, .env.example and the
// assistant must agree.
func TestInitialLanguageDefaultIsTheSameEverywhere(t *testing.T) {
	cfg, err := config.FromEnv(func(k string) (string, bool) {
		return map[string]string{"DATABASE_URL": "x"}[k], k == "DATABASE_URL"
	})
	if err != nil {
		t.Fatal(err)
	}
	example, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^INITIAL_LANGUAGE=(\w+)`).FindSubmatch(example)
	if m == nil || string(m[1]) != cfg.InitialLanguage || DefaultLanguage != cfg.InitialLanguage {
		t.Fatalf("server %q, .env.example %q, assistant %q", cfg.InitialLanguage, m, DefaultLanguage)
	}
}

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCheckListsEveryMissingSettingOfAnExistingEnv(t *testing.T) {
	dir := writeEnv(t, "PUBLIC_URL=chat.example.com\n")
	out, err := runSetup(t, "", "--check", "--dir", dir)
	if err == nil {
		t.Fatalf("a broken .env passed:\n%s", out)
	}
	if strings.Count(out, "cannot start") != 1 {
		t.Errorf("not one block:\n%s", out)
	}
	for _, want := range []string{"POSTGRES_PASSWORD", "JWT_SECRET", "S3_SECRET_KEY", "PUBLIC_URL"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestCheckAcceptsTheEnvTheAssistantWrites(t *testing.T) {
	dir := t.TempDir()
	if _, err := runSetup(t, "", "-y", "--dir", dir, "--public-url", "https://chat.example.com", "--compose-template", repoCompose, "--skip-preflight"); err != nil {
		t.Fatal(err)
	}
	out, err := runSetup(t, "", "--check", "--dir", dir)
	if err != nil || !strings.Contains(out, "all required settings are present") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestCheckWithoutAnEnvSaysSo(t *testing.T) {
	if _, err := runSetup(t, "", "--check", "--dir", t.TempDir()); err == nil || !strings.Contains(err.Error(), "no .env") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetupOverAnExistingEnvExplainsWhatIsWrongWithIt(t *testing.T) {
	dir := writeEnv(t, "JWT_SECRET=short\n")
	out, err := runSetup(t, "", "-y", "--dir", dir, "--public-url", "https://chat.example.com", "--compose-template", repoCompose)
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(out, "JWT_SECRET") {
		t.Fatalf("err %v\n%s", err, out)
	}
}

// --local promises that nothing is reachable from outside: the web port, the
// voice UDP range and the relay are all published on loopback.
func TestLocalTrialPublishesEverythingOnLoopback(t *testing.T) {
	dir := t.TempDir()
	if _, err := runSetup(t, "", "-y", "--local", "--dir", dir, "--compose-template", repoCompose); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".env"))
	env, _ := godotenv.Unmarshal(string(raw))
	for _, k := range []string{"PUBLISH_ADDR", "WEBRTC_PUBLISH_ADDR", "TURN_PUBLISH_ADDR"} {
		if env[k] != "127.0.0.1" {
			t.Errorf("%s = %q", k, env[k])
		}
	}
	compose, _ := os.ReadFile(repoCompose)
	if !strings.Contains(string(compose), "${WEBRTC_PUBLISH_ADDR:-0.0.0.0}:${WEBRTC_UDP_PORT_MIN") {
		t.Error("the UDP range is not published on WEBRTC_PUBLISH_ADDR")
	}
}

// A second run as another user cannot read the first run's 0600 .env: it must
// still say the file was left alone, not fail on the permission.
func TestSetupOverAnUnreadableEnvStillRefusesCleanly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	dir := writeEnv(t, "JWT_SECRET=x\n")
	if err := os.Chmod(filepath.Join(dir, ".env"), 0); err != nil {
		t.Fatal(err)
	}
	out, err := runSetup(t, "", "-y", "--dir", dir, "--public-url", "https://chat.example.com", "--compose-template", repoCompose)
	if err == nil || !strings.Contains(err.Error(), "already exists and was left alone") || strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if _, err := runSetup(t, "", "--check", "--dir", dir); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("--check must report the unreadable file: %v", err)
	}
}
