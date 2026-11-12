package webbundle

// Hand-written sequences next to the generated ones: the 3.0.9 floor raise
// against a client that still holds 3.0.8, and a key change half applied.

import (
	"crypto/ed25519"
	"testing"
)

func caseClient(t *testing.T, builtin string) (*world, *modelClient) {
	w := newWorld(t, 1)
	raw := w.bundle(mustVersion(builtin), 1, mustVersion(baselineFloor), 0)
	c := newModelClient(w.root.Public().(ed25519.PublicKey), []ed25519.PublicKey{w.pub(0)}, raw)
	c.round(w.stmts[0].doc, w.stmts[0].sig, true, w.now)
	if !c.fresh {
		t.Fatal("the first statement was not taken")
	}
	return w, c
}

func served(c *modelClient, server string) string {
	var s *Version
	if server != "dev" {
		v := mustVersion(server)
		s = &v
	}
	if b := c.selectFor(1, s); b != nil {
		return b.m.Version
	}
	return "refused"
}

func wantServed(t *testing.T, c *modelClient, label string, want map[string]string) {
	t.Helper()
	for server, v := range want {
		if got := served(c, server); got != v {
			t.Errorf("%s: a %s server gets %s, want %s", label, server, got, v)
		}
	}
}

// TestFloorRaisedWhileHolding308: 3.0.9 raised the floor to itself; 3.0.10
// (no trailer) repeats it (scripts/security-floor.sh, hunt finding H-L11-02).
func TestFloorRaisedWhileHolding308(t *testing.T) {
	floor := func(v string) Version { return mustVersion(v) }
	t.Run("seen 3.0.9", func(t *testing.T) {
		w, c := caseClient(t, "3.0.8")
		if err := c.install(w.bundle(floor("3.0.9"), 1, floor("3.0.9"), 0)); err != nil {
			t.Fatal(err)
		}
		if err := c.install(w.bundle(floor("3.0.10"), 1, floor("3.0.9"), 0)); err != nil {
			t.Fatal(err)
		}
		c.restart()
		if fmtVersion(c.floor) != "3.0.9" || len(c.shelved) != 1 || c.shelved[0].m.Version != "3.0.8" {
			t.Fatalf("floor %s, shelved %d: the 3.0.9 floor must hold over 3.0.10's baseline and shelve 3.0.8", fmtVersion(c.floor), len(c.shelved))
		}
		wantServed(t, c, "after 3.0.10", map[string]string{"3.0.8": "3.0.10", "dev": "3.0.10"})
	})
	t.Run("offline for 3.0.9", func(t *testing.T) {
		w, c := caseClient(t, "3.0.8")
		// 3.0.10 repeats the 3.0.9 floor, so this client learns it without
		// having seen 3.0.9, and 3.0.8 is shelved on every contract.
		if err := c.install(w.bundle(floor("3.0.10"), 1, floor("3.0.9"), 0)); err != nil {
			t.Fatal(err)
		}
		c.restart()
		if fmtVersion(c.floor) != "3.0.9" || len(c.shelved) != 1 || c.shelved[0].m.Version != "3.0.8" {
			t.Fatalf("floor %s, shelved %d: 3.0.10 must carry the 3.0.9 floor and shelve 3.0.8", fmtVersion(c.floor), len(c.shelved))
		}
		wantServed(t, c, "after 3.0.10 only", map[string]string{"3.0.8": "3.0.10", "dev": "3.0.10"})
	})
	t.Run("frozen before 3.0.9", func(t *testing.T) {
		w, c := caseClient(t, "3.0.8")
		c.round(nil, nil, false, w.now)
		if err := c.install(w.bundle(floor("3.0.9"), 1, floor("3.0.9"), 0)); err == nil {
			t.Fatal("installed without a fresh statement")
		}
		// The documented freeze: the client keeps running what it has.
		wantServed(t, c, "frozen", map[string]string{"3.0.8": "3.0.8"})
	})
}

// TestKeyChangeHalfApplied: the workflow signs with a new key before a
// statement allows it; the release is refused, and once the statement
// arrives the old key's bundles and marks stop counting.
func TestKeyChangeHalfApplied(t *testing.T) {
	w, c := caseClient(t, "3.0.8")
	early := w.bundle(mustVersion("3.0.9"), 1, mustVersion("3.0.9"), 1)
	if err := c.install(early); err == nil {
		t.Fatal("a bundle of a key no statement allows was installed")
	}
	w.allowed[1], w.allowed[0], w.revoked[0] = true, false, true
	s := w.sign(nil)
	c.round(s.doc, s.sig, true, w.now)
	if len(c.held)+len(c.shelved) != 0 {
		t.Fatalf("bundles of the revoked key are still held: %d", len(c.held)+len(c.shelved))
	}
	if err := c.install(early); err != nil {
		t.Fatalf("the new key's release after the statement: %v", err)
	}
	old := w.stmts[0]
	c.round(old.doc, old.sig, true, w.now)
	if c.fresh || served(c, "3.0.8") != "3.0.9" {
		t.Fatalf("an older statement replayed changed the state: fresh=%v served=%s", c.fresh, served(c, "3.0.8"))
	}
}
