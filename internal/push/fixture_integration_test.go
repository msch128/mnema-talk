//go:build integration

package push

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

// member is an account with a browser that has push on.
type member struct {
	id      uuid.UUID
	name    string
	browser browser
}

// env is a database with one category and channels, a permission state built
// by hand, a fake push service and a running delivery Service.
type env struct {
	t        *testing.T
	pool     *db.Pool
	perms    *permtest.Fixture
	push     *pushService
	svc      *Service
	category uuid.UUID
	general  uuid.UUID
	secret   uuid.UUID
}

func newEnv(t *testing.T, status int) *env { return newEnvWith(t, status, 30*time.Millisecond) }

func newEnvWith(t *testing.T, status int, burst time.Duration) *env {
	t.Helper()
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	f := permtest.NewFixture()
	e := &env{t: t, pool: pool, perms: f, push: newPushService(t, status)}
	e.category = f.Category()
	e.general, e.secret = f.Channel(e.category), f.Channel(e.category)
	f.RoleOverwrite(true, e.secret, f.EveryoneID(), 0, perm.ViewChannel)
	ctx := context.Background()
	e.exec(`INSERT INTO categories (id, name) VALUES ($1, 'Talk')`, e.category)
	e.exec(`INSERT INTO channels (id, category_id, name) VALUES ($1, $2, 'general'), ($3, $2, 'secret')`, e.general, e.category, e.secret)
	e.svc = &Service{
		DB: pool, Perms: f.Service(), Previews: true, Burst: burst, RetryWait: 10 * time.Millisecond,
		Sender: &Sender{Keys: testKeys(t), Subject: "mailto:ops@example.com", HTTP: e.push.Client()},
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { e.svc.Run(runCtx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return e
}

// member creates an account and registers its browser at the fake push service.
func (e *env) member(name string) member {
	e.t.Helper()
	m := member{name: name, browser: newBrowser(e.t)}
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO users (username, display_name, password_hash, locale) VALUES ($1, $2, 'x', 'en') RETURNING id`, name, "Display "+name).Scan(&m.id); err != nil {
		e.t.Fatal(err)
	}
	e.subscribe(m, "/device/"+name)
	return m
}

func (e *env) subscribe(m member, path string) uuid.UUID {
	e.t.Helper()
	sub, err := upsert(context.Background(), e.pool, m.id, newSubscription{
		endpoint: e.push.URL + path, p256dh: m.browser.p256dh(), auth: m.browser.secret(), label: "Test browser",
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return sub.ID
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

// message is a new message of author in channel, as the hub publishes it.
func message(author member, channel uuid.UUID, content string, mentions ...member) chat.Message {
	m := chat.Message{
		ID: uuid.New(), ChannelID: channel, UserID: author.id, Username: author.name, DisplayName: "Display " + author.name,
		Content: content, Mentions: []uuid.UUID{}, CreatedAt: time.Now(),
	}
	for _, who := range mentions {
		m.Mentions = append(m.Mentions, who.id)
	}
	return m
}

func (e *env) publish(msg chat.Message) { e.svc.Observe(msg.ChannelID, events.MessageCreate, msg) }

// pushedTo waits until count requests reached the device path, and returns them.
func (e *env) pushedTo(path string, count int) []received {
	e.t.Helper()
	var got []received
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got = e.requestsTo(path)
		if len(got) >= count {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.t.Fatalf("%s: %d pushes after 5 s, want %d", path, len(got), count)
	return nil
}

func (e *env) requestsTo(path string) []received {
	var out []received
	for _, r := range e.push.requests() {
		if r.path == path {
			out = append(out, r)
		}
	}
	return out
}

// settle gives pushes still on their way time to arrive, for "nothing came" checks.
func settle() { time.Sleep(150 * time.Millisecond) }

// content decrypts a push as the member's browser would.
func (e *env) content(m member, r received) Payload {
	e.t.Helper()
	var p Payload
	if err := json.Unmarshal(m.browser.open(e.t, r.body), &p); err != nil {
		e.t.Fatal(err)
	}
	return p
}
