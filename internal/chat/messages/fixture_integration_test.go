//go:build integration

package messages

import (
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

// fixture is a member with one text channel holding one message.
type fixture struct {
	p   *db.Pool
	u   *auth.User
	ch  uuid.UUID
	msg uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	p, u := chattest.NewMember(t)
	ch := chattest.NewChannel(t, p, "coverage-chat")
	return fixture{p: p, u: u, ch: ch, msg: chattest.NewMessage(t, p, ch, u.ID, "hello world")}
}
