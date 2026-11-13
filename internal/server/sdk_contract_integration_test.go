//go:build integration

package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSDKContract runs the contract tests of the official bot SDKs
// (sdk/typescript, a checkout of github.com/mnema-xyz/bot-sdk, and sdk/go)
// against this server: a real router on a real database, a bot with a token
// and a channel, and a signed-in member who runs its commands and clicks its
// buttons. It needs Node 24 or newer and runs only
// with SDK_CONTRACT=1 (CI's sdk job sets it), so the plain integration run
// stays free of Node; with the variable set, a missing tool fails the test.
func TestSDKContract(t *testing.T) {
	if os.Getenv("SDK_CONTRACT") != "1" {
		t.Skip("set SDK_CONTRACT=1 to run the SDK contract tests (needs Node 24+)")
	}
	f := newBotFixture(t, true)
	const intents = 1 | 512 | 1024 // GUILDS | GUILD_MESSAGES | GUILD_MESSAGE_REACTIONS
	wantStatus(t, "intents", f.owner.patch(f.botPath, map[string]any{"allowed_intents": intents}), http.StatusOK)
	env := append(os.Environ(),
		"MNEMA_CONTRACT_URL="+f.a.srv.URL,
		"MNEMA_CONTRACT_TOKEN="+f.token.Secret,
		"MNEMA_CONTRACT_CHANNEL="+f.open.String(),
		"MNEMA_CONTRACT_MEMBER_COOKIE="+cookieHeader(f.owner, f.a),
	)
	root := filepath.Join("..", "..", "sdk")
	t.Run("typescript", func(t *testing.T) {
		runContract(t, env, filepath.Join(root, "typescript"), "npm", "run", "test:contract")
	})
	t.Run("go", func(t *testing.T) {
		runContract(t, env, filepath.Join(root, "go"), "go", "test", "-count=1", "-tags=contract", "-run", "Contract", "./...")
	})
}

// cookieHeader is the Cookie header of a signed-in client.
func cookieHeader(c *client, a *app) string {
	var parts []string
	for _, ck := range c.http.Jar.Cookies(mustURL(a.srv.URL)) {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	return strings.Join(parts, "; ")
}

// runContract runs one SDK's contract suite in dir and fails with its output.
func runContract(t *testing.T, env []string, dir, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s in %s: %v\n%s", name, strings.Join(args, " "), dir, err, out)
	}
	t.Logf("%s", out)
}
