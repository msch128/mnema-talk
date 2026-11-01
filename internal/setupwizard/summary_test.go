package setupwizard

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// fixedSecrets keeps the snapshots stable.
var fixedSecrets = Secrets{AdminPassword: "example-password"}

func summaryFor(t *testing.T, o Options, container bool) string {
	t.Helper()
	o, err := o.normalize()
	if err != nil {
		t.Fatal(err)
	}
	g := Generated{EnvPath: "/srv/mnema/.env", ComposePath: "/srv/mnema/docker-compose.yml", Secrets: fixedSecrets}
	var out bytes.Buffer
	printSummary(&out, o, g, nil, container)
	return out.String()
}

// compareGolden is the snapshot test of the assistant's closing text: run the
// tests with -update after changing it on purpose, and read the diff.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/setupwizard -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s changed; run go test ./internal/setupwizard -update if that is intended.\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func TestSummarySnapshots(t *testing.T) {
	for name, tc := range map[string]struct {
		o         Options
		container bool
	}{
		"summary_https":     {Options{PublicURL: "https://chat.example.com", AdminUsername: "owner"}, false},
		"summary_container": {Options{PublicURL: "https://chat.example.com"}, true},
		"summary_local":     {Options{Local: true}, false},
		"summary_lan_http":  {Options{PublicURL: "http://192.168.1.20:8080"}, false},
	} {
		t.Run(name, func(t *testing.T) { compareGolden(t, name, summaryFor(t, tc.o, tc.container)) })
	}
}

// The checklist is what a newcomer follows: whatever else changes, it names the
// start, the URL, the owner, the password change, the three ports and the
// connection check, in this order.
func TestSummaryChecklistOrderAndContent(t *testing.T) {
	got := summaryFor(t, Options{PublicURL: "https://chat.example.com", AdminUsername: "owner"}, false)
	order := []string{
		"What to do next:", "docker compose up -d --wait", "reverse proxy", "Open https://chat.example.com",
		`Sign in as "owner"`, "Change the password", "never 8080", "443/tcp", "50000-50050/udp", "3479/udp and 3479/tcp",
		`"Test my connection"`, "mobile data", "internet upload", "no number is shown here", "mnema-talk doctor",
	}
	at := 0
	for _, want := range order {
		i := strings.Index(got[at:], want)
		if i < 0 {
			t.Fatalf("%q missing or out of order after offset %d:\n%s", want, at, got)
		}
		at += i + len(want)
	}
}

func TestLocalSummaryOffersNoRouterWork(t *testing.T) {
	got := summaryFor(t, Options{Local: true}, false)
	for _, bad := range []string{"Forward", "reverse proxy", "443/tcp"} {
		if strings.Contains(got, bad) {
			t.Errorf("a trial on this computer mentions %q:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "Open http://localhost:8080") {
		t.Errorf("the trial URL is missing:\n%s", got)
	}
}

func TestSummaryUsesTheRealPort(t *testing.T) {
	got := summaryFor(t, Options{PublicURL: "http://localhost:9000"}, false)
	if !strings.Contains(got, "Open http://localhost:9000") {
		t.Errorf("port lost:\n%s", got)
	}
	got = summaryFor(t, Options{PublicURL: "https://chat.example.com"}, false)
	if !strings.Contains(got, "http://127.0.0.1:8080") {
		t.Errorf("proxy target missing:\n%s", got)
	}
}
