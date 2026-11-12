package webbundle

// Generated sequences over the update trust chain (lane 11 of the defect
// hunt): releases with raised and baseline floors, statements renewed and
// replayed, keys rotated in order and half applied, a leaked key forging a
// high bundle, cut downloads, clock steps and restarts. The oracle states the
// property of doc/desktop-trusted-client.md directly and keeps its own record
// of what the client was shown: the client never selects a bundle signed by a
// key it was told is retired, nor one below the highest floor or version a
// still-trusted key declared to it.
//
// Smoke: a few hundred sequences with the normal test run. Deep:
// WEBBUNDLE_TRUST_SWEEP=<seeds> (and WEBBUNDLE_TRUST_SHARD=i/n).

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type obligation struct {
	key            string
	api            int
	version, floor Version
}

// oracle is the record of what the client was shown, kept apart from the model.
type oracle struct {
	trust       *Trust
	retired     map[string]bool
	obligations []obligation
}

// shown records a statement the client could take at its own clock: a valid
// one with a higher serial than the last.
func (o *oracle) shown(s statement, clientNow time.Time) {
	if s.t.Expired(clientNow) || (o.trust != nil && s.t.Serial <= o.trust.Serial) {
		return
	}
	t := s.t
	o.trust = &t
	for _, id := range t.RevokedBundleKeys {
		o.retired[id] = true
	}
	o.cap(t.MaxVersion)
}

// cap lowers what was recorded before a statement with max_version (the
// documented way to clear a forged mark).
func (o *oracle) cap(caps map[string]string) {
	var highest Version
	for _, v := range caps {
		highest = vmax(highest, mustVersion(v))
	}
	for i, ob := range o.obligations {
		if len(caps) > 0 {
			o.obligations[i].floor = vmin(ob.floor, highest)
		}
		if v, ok := caps[itoaAPI(ob.api)]; ok {
			o.obligations[i].version = vmin(ob.version, mustVersion(v))
		}
	}
}

func (o *oracle) verified(b *modelBundle) {
	o.obligations = append(o.obligations, obligation{key: b.m.KeyID, api: b.m.WebClientAPI, version: b.version, floor: b.floor})
}

func (o *oracle) allowed(id string, pinned []ed25519.PublicKey) bool {
	if o.retired[id] {
		return false
	}
	keys := pinned
	if o.trust != nil {
		keys = nil
		for _, h := range o.trust.BundleKeys {
			raw, _ := decodeHexKey(h)
			keys = append(keys, raw)
		}
	}
	for _, k := range keys {
		if KeyID(k) == id {
			return true
		}
	}
	return false
}

// check is the property.
func (o *oracle) check(b *modelBundle, pinned []ed25519.PublicKey) error {
	if !o.allowed(b.m.KeyID, pinned) {
		return fmt.Errorf("selected %s signed by %s, which is retired or not allowed", b.m.Version, b.m.KeyID)
	}
	for _, ob := range o.obligations {
		if !o.allowed(ob.key, pinned) {
			continue
		}
		if vcmp(b.version, ob.floor) < 0 {
			return fmt.Errorf("selected %s below the floor %s declared by %s", b.m.Version, fmtVersion(ob.floor), ob.key)
		}
		if ob.api == b.m.WebClientAPI && vcmp(b.version, ob.version) < 0 {
			return fmt.Errorf("selected %s below %s already verified for contract %d", b.m.Version, fmtVersion(ob.version), ob.api)
		}
	}
	return nil
}

// chainRun is one sequence.
type chainRun struct {
	w      *world
	c      *modelClient
	o      *oracle
	pinned []ed25519.PublicKey
	counts map[string]int
	trace  []string
	// pendingRotation is a key the release workflow already signs with but
	// that no statement allows yet (a key change half applied).
	pendingRotation int
}

func newRun(t *testing.T, seed int64, counts map[string]int) *chainRun {
	w := newWorld(t, seed)
	pinned := []ed25519.PublicKey{w.pub(0)}
	builtin := w.release(false)
	r := &chainRun{w: w, pinned: pinned, counts: counts, pendingRotation: -1,
		o: &oracle{retired: map[string]bool{}}}
	r.c = newModelClient(w.root.Public().(ed25519.PublicKey), pinned, builtin.raw)
	for _, b := range r.c.held {
		r.o.verified(b)
	}
	return r
}

func (r *chainRun) note(kind, detail string) {
	r.counts[kind]++
	r.trace = append(r.trace, kind+" "+detail)
}

// offer hands a bundle to the client's install path.
func (r *chainRun) offer(kind string, raw []byte) {
	err := r.c.install(raw)
	r.note(kind, fmt.Sprintf("len=%d err=%v", len(raw), err))
	if err != nil {
		return
	}
	r.counts[kind+": installed"]++
	b := r.c.held[0]
	for _, h := range r.c.held {
		if string(h.raw) == string(raw) {
			b = h
		}
	}
	r.o.verified(b)
	if r.o.trust == nil || r.o.trust.Expired(r.w.now) {
		r.counts["installed while the statement in force had expired (true time)"]++
	}
}

func (r *chainRun) deliver(kind string, s statement) {
	r.c.round(s.doc, s.sig, true, r.w.clientNow())
	r.o.shown(s, r.w.clientNow())
	r.note(kind, fmt.Sprintf("serial=%d fresh=%v", s.t.Serial, r.c.fresh))
}

func (r *chainRun) newest() statement { return r.w.stmts[len(r.w.stmts)-1] }

func (r *chainRun) step() {
	w := r.w
	switch p := w.rng.Intn(100); {
	case p < 22:
		rel := w.release(w.rng.Intn(10) < 3)
		if w.rng.Intn(10) < 8 {
			r.offer("release", rel.raw)
		}
	case p < 36:
		if w.rng.Intn(5) == 0 {
			r.c.round(nil, nil, false, w.clientNow())
			r.note("fetch blocked", "")
		} else {
			r.deliver("trust round", r.newest())
		}
	case p < 42:
		r.deliver("old statement replayed", w.stmts[w.rng.Intn(len(w.stmts))])
	case p < 50:
		r.offer("old bundle replayed", w.releases[w.rng.Intn(len(w.releases))].raw)
	case p < 55:
		raw := w.releases[len(w.releases)-1].raw
		r.offer("cut download", raw[:w.rng.Intn(len(raw))])
	default:
		r.rareStep(p)
	}
}

func (r *chainRun) rareStep(p int) {
	w := r.w
	switch {
	case p < 61:
		r.rotate()
	case p < 64:
		r.forge()
	case p < 68:
		w.skew -= time.Duration(1+w.rng.Intn(200)) * 24 * time.Hour
		r.note("clock set back", w.skew.String())
	case p < 70:
		w.skew = 0
		r.note("clock corrected", "")
	case p < 78:
		w.now = w.now.Add(time.Duration(1+w.rng.Intn(60*24)) * time.Hour)
		r.note("time passes", w.now.Format(time.RFC3339))
	case p < 82:
		w.sign(nil)
		r.note("statement renewed", "")
	case p < 87:
		r.c.restart()
		r.note("restart", "")
	case p < 89:
		r.capForged()
	case p < 90:
		if w.nextAPI == 1 {
			w.nextAPI, w.next, w.minSrv = 2, [3]uint32{4, 0, 0}, "0.7.16"
			r.note("new contract", "")
		}
	}
}

// rotate changes the CI key: in order (statement first), half applied (the
// workflow signs with a key no statement allows yet) or completing a half
// applied change.
func (r *chainRun) rotate() {
	w := r.w
	if r.pendingRotation >= 0 {
		k := r.pendingRotation
		w.allowed[k], w.allowed[w.ci], w.revoked[w.ci] = true, false, true
		w.ci, r.pendingRotation = k, -1
		w.sign(nil)
		r.note("rotation completed", strconv.Itoa(k))
		return
	}
	k := w.unusedKey()
	if k < 0 || len(w.allowed) == 0 {
		return
	}
	if w.rng.Intn(2) == 0 {
		w.allowed[k], w.allowed[w.ci], w.revoked[w.ci] = true, false, true
		w.ci = k
		w.sign(nil)
		r.note("rotation in order", strconv.Itoa(k))
		return
	}
	r.pendingRotation = k
	old := w.ci
	w.ci = k
	// The old key stays allowed until the statement follows.
	r.note("rotation half applied", fmt.Sprintf("%d->%d", old, k))
}

// forge: the CI key leaked; the attacker serves a high bundle with a high
// floor signed by it. The owner revokes the key (rotation) afterwards.
func (r *chainRun) forge() {
	w := r.w
	last := w.releases[len(w.releases)-1]
	v := Version{last.version[0], last.version[1] + 5, 0}
	raw := w.bundle(v, last.api, v, w.ci)
	w.leaked[w.ci] = true
	r.offer("forged bundle by a leaked key", raw)
	if r.pendingRotation < 0 {
		r.rotate()
	}
}

// capForged signs a statement whose max_version clears marks above the
// newest real release, as the document says to after a forgery.
func (r *chainRun) capForged() {
	w := r.w
	if len(w.leaked) == 0 {
		return
	}
	last := w.releases[len(w.releases)-1]
	w.sign(map[string]string{itoaAPI(last.api): fmtVersion(last.version)})
	r.note("statement with a cap", fmtVersion(last.version))
}

// selectAll asks for a bundle as servers of several versions would.
func (r *chainRun) selectAll() error {
	w := r.w
	servers := []*Version{nil}
	for i := 0; i < 2; i++ {
		v := w.releases[w.rng.Intn(len(w.releases))].version
		servers = append(servers, &v)
	}
	for _, api := range []int{1, 2} {
		for _, s := range servers {
			b := r.c.selectFor(api, s)
			if b == nil {
				r.counts["select: refused"]++
				continue
			}
			r.counts["select: served"]++
			if err := r.o.check(b, r.pinned); err != nil {
				return err
			}
		}
	}
	return nil
}

func runSequence(t *testing.T, seed int64, steps int, counts map[string]int, mutant string) error {
	r := newRun(t, seed, counts)
	r.c.mutant = mutant
	for i := 0; i < steps; i++ {
		r.step()
		if err := r.selectAll(); err != nil {
			return fmt.Errorf("seed %d step %d: %w\ntrace:\n  %s", seed, i, err, strings.Join(r.trace, "\n  "))
		}
	}
	return nil
}

func sweepRange(t *testing.T) (from, to int64) {
	n := int64(40)
	if v := os.Getenv("WEBBUNDLE_TRUST_SWEEP"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil || parsed < 1 {
			t.Fatalf("WEBBUNDLE_TRUST_SWEEP=%q is not a seed count", v)
		}
		n = parsed
	}
	from, to = 0, n
	if s := os.Getenv("WEBBUNDLE_TRUST_SHARD"); s != "" {
		var i, k int64
		if _, err := fmt.Sscanf(s, "%d/%d", &i, &k); err != nil || k < 1 || i < 0 || i >= k {
			t.Fatalf("WEBBUNDLE_TRUST_SHARD=%q is not i/n", s)
		}
		from, to = n*i/k, n*(i+1)/k
	}
	return from, to
}

// perturbations every sweep must have exercised, with the outcome that shows
// the step really happened.
var mustHappen = []string{
	"release: installed", "old statement replayed", "old bundle replayed", "cut download",
	"rotation in order", "rotation half applied", "rotation completed",
	"forged bundle by a leaked key: installed", "statement with a cap", "clock set back",
	"fetch blocked", "restart", "new contract", "release", "select: served", "select: refused",
}

func TestTrustChainSequences(t *testing.T) {
	from, to := sweepRange(t)
	counts := map[string]int{}
	for seed := from; seed < to; seed++ {
		if err := runSequence(t, seed, 120, counts, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range mustHappen {
		if counts[kind] == 0 {
			t.Errorf("no sequence exercised %q: the sweep proves nothing about it", kind)
		}
	}
	// A replayed release is installed only when the client never held
	// anything newer of its contract (it was offline then); the oracle
	// checks every selection after it.
	if n := counts["cut download: installed"]; n > 0 {
		t.Errorf("%d cut downloads were installed", n)
	}
	t.Logf("seeds %d..%d: %v", from, to, counts)
}

// TestTrustChainOracleCatchesMutants shows the oracle is not blind: a model
// that skips one documented rule must be caught within the smoke seeds.
func TestTrustChainOracleCatchesMutants(t *testing.T) {
	for _, mutant := range []string{"keep revoked bundles", "ignore the floor", "ignore the high-water mark", "trust compiled-in keys"} {
		caught := false
		for seed := int64(0); seed < 40 && !caught; seed++ {
			caught = runSequence(t, seed, 120, map[string]int{}, mutant) != nil
		}
		if !caught {
			t.Errorf("the oracle did not catch a model that does %q", mutant)
		}
	}
}
