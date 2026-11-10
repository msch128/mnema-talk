//go:build integration

package server

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/cookiejar"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Every permission change drops the whole permission snapshot, and the next
// question reloads it from the database. Accepting the rules is such a change
// and an everyday one. This test measures what that costs in a large
// community: one load, a run of rule acceptances, and the latency of sending
// messages meanwhile. It prints the numbers quoted in
// doc/architecture.md#the-permission-snapshot-with-10000-members (run with -v).
//
// CI runs a small community. PERM_SNAPSHOT_SCALE=full runs the documented
// size and then also fails when a figure is above its limit.

// snapshotScale is the size of the community the measurement runs in. Each
// member holds two roles, so there are twice as many assignments as members.
type snapshotScale struct {
	members, roles, conversations, acceptances int
	isolatedLoads                              int
	baseline                                   time.Duration
}

const (
	// maxSnapshotLoad is the longest one load may take before the design of
	// the snapshot has to change.
	maxSnapshotLoad = 50 * time.Millisecond
	// maxSendP99 is the limit for the 99th percentile of sending a message
	// while members accept the rules one after the other.
	maxSendP99 = 200 * time.Millisecond

	// integrationSecret is the session secret newApp configures.
	integrationSecret = "integration-test-secret-integration-test"

	scaleWriters        = 4
	scaleWriterSessions = 10
	// scaleWriterPause keeps every writing session far below the API's
	// per-member write budget.
	scaleWriterPause = 25 * time.Millisecond
)

func snapshotScaleFromEnv() (snapshotScale, bool) {
	if os.Getenv("PERM_SNAPSHOT_SCALE") == "full" {
		return snapshotScale{members: 10000, roles: 30, conversations: 5000, acceptances: 500, isolatedLoads: 20, baseline: 5 * time.Second}, true
	}
	return snapshotScale{members: 300, roles: 5, conversations: 100, acceptances: 10, isolatedLoads: 3, baseline: 300 * time.Millisecond}, false
}

// loadTracer watches the permission service's own pool. Nothing but snapshot
// loads runs there in this test, each in one transaction, so every BEGIN is
// one load and the rows returned are the rows the loads read.
type loadTracer struct{ loads, rows atomic.Int64 }

func (l *loadTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(data.SQL)), "begin") {
		l.loads.Add(1)
	}
	return ctx
}

func (l *loadTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.CommandTag.Select() {
		l.rows.Add(data.CommandTag.RowsAffected())
	}
}

// tracedPermissions returns a permission service that reads the same
// database as source through a pool of its own, watched by the tracer.
func tracedPermissions(t *testing.T, source *db.Pool) (*perm.Service, *loadTracer) {
	t.Helper()
	tracer := &loadTracer{}
	config := source.Config()
	config.MaxConns, config.MinConns, config.MinIdleConns = 2, 0, 0
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return perm.NewService(&db.Pool{Pool: pool}), tracer
}

// seedCommunity fills the database with the members, roles, assignments and
// conversations of s. The first s.acceptances members have not accepted the
// rules yet; everybody else has.
func (a *app) seedCommunity(s snapshotScale) {
	a.t.Helper()
	a.runSQL(`UPDATE instance_settings SET rules_text = 'Be kind.', rules_version = 1`)
	a.runSQL(`INSERT INTO users (username, display_name, password_hash, created_at, rules_accepted_version)
	        SELECT 'member' || g, 'member' || g, 'x', NOW() - interval '1 day', CASE WHEN g <= $2 THEN 0 ELSE 1 END
	        FROM generate_series(1, $1) g`, s.members, s.acceptances)
	a.runSQL(`INSERT INTO roles (name, position, permissions)
	        SELECT 'scale-role-' || g, g + 1, 0 FROM generate_series(1, $1) g`, s.roles)
	a.runSQL(`WITH m AS (SELECT id, row_number() OVER (ORDER BY id) AS n FROM users WHERE username LIKE 'member%'),
	             r AS (SELECT id, row_number() OVER (ORDER BY position) - 1 AS n FROM roles WHERE name LIKE 'scale-role-%')
	        INSERT INTO user_roles (user_id, role_id)
	        SELECT m.id, r.id FROM m JOIN r ON r.n = m.n % $1 OR r.n = (m.n + 1) % $1`, s.roles)
	a.runSQL(`WITH m AS (SELECT id, row_number() OVER (ORDER BY id) AS n FROM users WHERE username LIKE 'member%'),
	             pairs AS (SELECT x.id AS a, y.id AS b FROM m x JOIN m y ON y.n = x.n + $1 WHERE x.n <= $1),
	             made AS (INSERT INTO channels (number, name, type, dm_key, dm_created_by) OVERRIDING SYSTEM VALUE
	                      SELECT nextval('dm_number_seq'), '', 'dm', LEAST(a::text, b::text) || ':' || GREATEST(a::text, b::text), a
	                      FROM pairs RETURNING id, dm_created_by)
	        INSERT INTO dm_members (channel_id, user_id, added_by)
	        SELECT made.id, who.id, made.dm_created_by
	        FROM made JOIN pairs ON pairs.a = made.dm_created_by CROSS JOIN LATERAL (VALUES (pairs.a), (pairs.b)) AS who(id)`, s.conversations)
	a.runSQL(`ANALYZE`)
}

// membersWhere returns the ids of the seeded members matching cond, in a
// stable order.
func (a *app) membersWhere(cond string, limit int) []uuid.UUID {
	a.t.Helper()
	rows, err := a.db.Query(context.Background(), `SELECT id FROM users WHERE username LIKE 'member%' AND `+cond+` ORDER BY id LIMIT $1`, limit)
	if err != nil {
		a.t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		a.t.Fatal(err)
	}
	if len(ids) != limit {
		a.t.Fatalf("found %d members where %s, want %d", len(ids), cond, limit)
	}
	return ids
}

// sessionFor signs a member in without the login route (and its per-address
// limit): the session cookie is issued with the server's own secret.
func (a *app) sessionFor(id uuid.UUID) *client {
	a.t.Helper()
	token, err := auth.IssueToken(id, 0, integrationSecret, time.Hour)
	if err != nil {
		a.t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(mustURL(a.srv.URL), []*http.Cookie{{Name: "mnema_session", Value: token, Path: "/"}})
	return &client{a: a, http: &http.Client{Jar: jar}}
}

// latencies collects request durations from several goroutines.
type latencies struct {
	mu     sync.Mutex
	took   []time.Duration
	failed []string
}

func (l *latencies) add(d time.Duration, failure string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.took = append(l.took, d)
	if failure != "" {
		l.failed = append(l.failed, failure)
	}
}

// percentile returns the p-th percentile (nearest rank) of d.
func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sorted := slices.Clone(d)
	slices.Sort(sorted)
	return sorted[max(int(math.Ceil(p/100*float64(len(sorted))))-1, 0)]
}

func (l *latencies) String() string {
	return fmt.Sprintf("%d requests, p50 %v, p99 %v, max %v", len(l.took), percentile(l.took, 50), percentile(l.took, 99), percentile(l.took, 100))
}

// sendWhile has the writers send messages to ch until during returns, and
// returns how long each POST /api/channels/{id}/messages took.
func (a *app) sendWhile(writers [][]*client, ch uuid.UUID, during func()) *latencies {
	a.t.Helper()
	sends := &latencies{}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for _, sessions := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				start := time.Now()
				res := sessions[i%len(sessions)].post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "hello"})
				failure := ""
				if res.status != http.StatusCreated {
					failure = fmt.Sprintf("%d %s", res.status, res.body)
				}
				sends.add(time.Since(start), failure)
				time.Sleep(scaleWriterPause)
			}
		}()
	}
	during()
	close(stop)
	wg.Wait()
	if len(sends.failed) > 0 {
		a.t.Fatalf("%d of %d sends failed, first: %s", len(sends.failed), len(sends.took), sends.failed[0])
	}
	return sends
}

// writerSessions signs in members who have accepted the rules, a separate
// set for each writer.
func (a *app) writerSessions() [][]*client {
	a.t.Helper()
	ids := a.membersWhere(`rules_accepted_version > 0`, scaleWriters*scaleWriterSessions)
	writers := make([][]*client, scaleWriters)
	for i, id := range ids {
		writers[i%scaleWriters] = append(writers[i%scaleWriters], a.sessionFor(id))
	}
	return writers
}

// isolatedLoads drops the snapshot and reloads it n times with nothing else
// going on, and returns how long each load took.
func isolatedLoads(t *testing.T, perms *perm.Service, n int) []time.Duration {
	t.Helper()
	var took []time.Duration
	for i := 0; i < n; i++ {
		perms.Invalidate()
		start := time.Now()
		if _, err := perms.Snapshot(context.Background()); err != nil {
			t.Fatal(err)
		}
		took = append(took, time.Since(start))
	}
	return took
}

// acceptRules has each member accept the rules, one after the other, and
// returns how long each POST /api/onboarding/accept took.
func (a *app) acceptRules(members []*client) *latencies {
	a.t.Helper()
	accepts := &latencies{}
	for _, m := range members {
		start := time.Now()
		res := m.post("/api/onboarding/accept", map[string]any{"version": 1})
		if res.status != http.StatusNoContent {
			a.t.Fatalf("accept the rules: %d %s", res.status, res.body)
		}
		accepts.add(time.Since(start), "")
	}
	return accepts
}

func TestAcceptingTheRulesTakesEffectAtOnceInALargeCommunity(t *testing.T) {
	scale, full := snapshotScaleFromEnv()
	var (
		perms  *perm.Service
		tracer *loadTracer
	)
	a := newAppWithDeps(t, true, nil, func(d *Deps) {
		perms, tracer = tracedPermissions(t, d.DB)
		d.Perms = perms
	})
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "lobby", "text")
	a.seedCommunity(scale)
	writers := a.writerSessions()
	var newcomers []*client
	for _, id := range a.membersWhere(`rules_accepted_version = 0`, scale.acceptances) {
		newcomers = append(newcomers, a.sessionFor(id))
	}

	// One load, alone.
	loadsBefore, rowsBefore := tracer.loads.Load(), tracer.rows.Load()
	alone := isolatedLoads(t, perms, scale.isolatedLoads)
	rowsPerLoad := (tracer.rows.Load() - rowsBefore) / (tracer.loads.Load() - loadsBefore)
	t.Logf("community: %d members, %d roles, %d role assignments, %d conversations", scale.members, scale.roles, 2*scale.members, scale.conversations)
	t.Logf("one snapshot load: %d rows, median %v, min %v, max %v (%d loads)", rowsPerLoad, percentile(alone, 50), percentile(alone, 0), percentile(alone, 100), len(alone))

	// Sending messages while nothing changes.
	calm := a.sendWhile(writers, ch, func() { time.Sleep(scale.baseline) })
	t.Logf("sending without permission changes: %v", calm)

	// A member who has not accepted the rules may not write.
	if res := newcomers[0].post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "too early"}); res.status != http.StatusForbidden {
		t.Fatalf("send before accepting the rules: %d %s", res.status, res.body)
	}

	// The acceptances in a row, with messages being sent meanwhile.
	var accepts *latencies
	var burst time.Duration
	loadsBefore = tracer.loads.Load()
	busy := a.sendWhile(writers, ch, func() {
		start := time.Now()
		accepts = a.acceptRules(newcomers)
		burst = time.Since(start)
	})
	loads := tracer.loads.Load() - loadsBefore
	t.Logf("%d rule acceptances in a row: %v in total, %d snapshot loads, accept %v", scale.acceptances, burst, loads, accepts)
	t.Logf("sending during the acceptances: %v", busy)

	// Each of them may write with their very next request.
	for _, m := range []*client{newcomers[0], newcomers[len(newcomers)-1]} {
		if res := m.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "hello"}); res.status != http.StatusCreated {
			t.Fatalf("send after accepting the rules: %d %s", res.status, res.body)
		}
	}
	if !full {
		return
	}
	if got := percentile(alone, 50); got > maxSnapshotLoad {
		t.Errorf("one snapshot load takes %v, limit %v", got, maxSnapshotLoad)
	}
	if got := percentile(busy.took, 99); got > maxSendP99 {
		t.Errorf("p99 of sending during the acceptances is %v, limit %v", got, maxSendP99)
	}
}
