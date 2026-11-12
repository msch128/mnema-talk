package webbundle

// A reference model of the desktop client's update trust rules, as
// doc/desktop-trusted-client.md states them ("Key revocation", "Rollback
// protection", "Compatibility"). The desktop client lives in another
// repository; this model is written from the document, and its hand-written
// cases (trustchain_cases_test.go) give the outcomes that client's Linux
// implementation gave on the same bundles when probed. It verifies every byte
// with this package's Verify and VerifyTrust, so the generated sequences in
// trustchain_test.go run the Go verifier on real signed artifacts.

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"time"
)

func vcmp(a, b Version) int {
	for i := range a {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}

func vmax(a, b Version) Version {
	if vcmp(a, b) >= 0 {
		return a
	}
	return b
}

func vmin(a, b Version) Version {
	if vcmp(a, b) <= 0 {
		return a
	}
	return b
}

func mustVersion(s string) Version {
	v, err := ParseVersion(s)
	if err != nil {
		panic(err)
	}
	return v
}

// modelBundle is a verified bundle the model holds, with its bytes so a
// restart verifies it again.
type modelBundle struct {
	raw     []byte
	m       *Manifest
	version Version
	floor   Version
	minSrv  Version
	builtin bool
}

// modelClient is the documented client state.
type modelClient struct {
	pinned    []ed25519.PublicKey
	root      ed25519.PublicKey
	minSerial uint64
	trust     *Trust
	trustDoc  []byte
	revoked   map[string]bool
	fresh     bool
	held      []*modelBundle // served, newest first
	shelved   []*modelBundle // below the floor
	floors    map[string]Version
	marks     map[int]map[string]Version
	keys      []ed25519.PublicKey
	floor     Version
	highWater map[int]Version
	// mutant names one rule the model skips (TestTrustChainOracleCatchesMutants).
	mutant string
}

func newModelClient(root ed25519.PublicKey, pinned []ed25519.PublicKey, builtin []byte) *modelClient {
	c := &modelClient{
		pinned: pinned, root: root, revoked: map[string]bool{},
		floors: map[string]Version{}, marks: map[int]map[string]Version{},
	}
	c.keys = c.effectiveKeys()
	if b := c.verify(builtin); b != nil {
		b.builtin = true
		c.held = append(c.held, b)
		c.raise(b)
	}
	c.recompute()
	c.applyFloor()
	return c
}

func (c *modelClient) verify(raw []byte) *modelBundle {
	if raw == nil {
		return nil
	}
	m, err := Verify(raw, c.keys)
	if err != nil {
		return nil
	}
	return &modelBundle{raw: raw, m: m, version: mustVersion(m.Version),
		floor: mustVersion(m.SecurityFloor), minSrv: mustVersion(m.MinServerVersion)}
}

// effectiveKeys: the statement's allowed keys minus every revocation seen,
// else the compiled-in keys.
func (c *modelClient) effectiveKeys() []ed25519.PublicKey {
	if c.trust == nil || c.mutant == "trust compiled-in keys" {
		return c.pinned
	}
	var out []ed25519.PublicKey
	for _, k := range c.pinnedOrListed() {
		if !c.revoked[KeyID(k)] {
			out = append(out, k)
		}
	}
	return out
}

func (c *modelClient) pinnedOrListed() []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, h := range c.trust.BundleKeys {
		raw, _ := decodeHexKey(h)
		out = append(out, raw)
	}
	return out
}

func (c *modelClient) trusted(id string) bool {
	for _, k := range c.keys {
		if KeyID(k) == id {
			return true
		}
	}
	return false
}

var errUnchanged = errors.New("unchanged")

// acceptTrust follows "Anti-rollback", "Authoritative", "Freshness" and the
// cumulative revocations.
func (c *modelClient) acceptTrust(doc, sig []byte, now time.Time) error {
	t, err := VerifyTrust(doc, sig, c.root)
	if err != nil {
		return err
	}
	if t.Expired(now) {
		return errors.New("expired")
	}
	if t.Serial < c.minSerial {
		return errors.New("below the release floor")
	}
	if c.trust != nil {
		if t.Serial < c.trust.Serial {
			return errors.New("older serial")
		}
		if t.Serial == c.trust.Serial {
			if bytes.Equal(doc, c.trustDoc) {
				return errUnchanged
			}
			return errors.New("same serial, other bytes")
		}
	}
	for _, h := range t.BundleKeys {
		raw, _ := decodeHexKey(h)
		if c.revoked[KeyID(raw)] {
			return errors.New("a revoked key is allowed again")
		}
	}
	for _, id := range t.RevokedBundleKeys {
		c.revoked[id] = true
	}
	c.trust, c.trustDoc = t, doc
	c.keys = c.effectiveKeys()
	c.reconcile()
	c.applyCaps(t.MaxVersion)
	return nil
}

// round is one trust round: a fetch result, then the freshness decision.
func (c *modelClient) round(doc, sig []byte, fetched bool, now time.Time) {
	ok := false
	if fetched {
		err := c.acceptTrust(doc, sig, now)
		ok = err == nil || errors.Is(err, errUnchanged)
	}
	c.fresh = ok && c.trust != nil && !c.trust.Expired(now)
}

func (c *modelClient) reconcile() {
	if c.mutant == "keep revoked bundles" {
		return
	}
	keep := func(list []*modelBundle) []*modelBundle {
		var out []*modelBundle
		for _, b := range list {
			if c.trusted(b.m.KeyID) {
				out = append(out, b)
			}
		}
		return out
	}
	c.held, c.shelved = keep(c.held), keep(c.shelved)
	c.recompute()
	c.applyFloor()
}

func (c *modelClient) applyCaps(caps map[string]string) {
	if len(caps) == 0 {
		return
	}
	var highest Version
	for api, v := range caps {
		cv := mustVersion(v)
		highest = vmax(highest, cv)
		n := atoiAPI(api)
		for k, m := range c.marks[n] {
			c.marks[n][k] = vmin(m, cv)
		}
	}
	for k, f := range c.floors {
		c.floors[k] = vmin(f, highest)
	}
	over := func(b *modelBundle) bool {
		v, ok := caps[itoaAPI(b.m.WebClientAPI)]
		return ok && vcmp(b.version, mustVersion(v)) > 0
	}
	c.held, c.shelved = dropIf(c.held, over), dropIf(c.shelved, over)
	c.recompute()
	c.applyFloor()
}

func dropIf(list []*modelBundle, f func(*modelBundle) bool) []*modelBundle {
	var out []*modelBundle
	for _, b := range list {
		if !f(b) {
			out = append(out, b)
		}
	}
	return out
}

func (c *modelClient) raise(b *modelBundle) {
	id := b.m.KeyID
	if f, ok := c.floors[id]; !ok || vcmp(b.floor, f) > 0 {
		c.floors[id] = b.floor
	}
	api := b.m.WebClientAPI
	if c.marks[api] == nil {
		c.marks[api] = map[string]Version{}
	}
	if m, ok := c.marks[api][id]; !ok || vcmp(b.version, m) > 0 {
		c.marks[api][id] = b.version
	}
}

func (c *modelClient) recompute() {
	c.floor = Version{}
	for id, f := range c.floors {
		if c.trusted(id) {
			c.floor = vmax(c.floor, f)
		}
	}
	c.highWater = map[int]Version{}
	for api, byKey := range c.marks {
		for id, m := range byKey {
			if c.trusted(id) {
				c.highWater[api] = vmax(c.highWater[api], m)
			}
		}
	}
}

func (c *modelClient) applyFloor() {
	all := append(append([]*modelBundle{}, c.held...), c.shelved...)
	c.held, c.shelved = nil, nil
	for _, b := range all {
		if vcmp(b.version, c.floor) >= 0 || c.mutant == "ignore the floor" {
			c.held = append(c.held, b)
		} else {
			c.shelved = append(c.shelved, b)
		}
	}
	sortNewest(c.held)
}

func sortNewest(list []*modelBundle) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && vcmp(list[j].version, list[j-1].version) > 0; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// install takes a downloaded bundle: only with a fresh statement, only when it
// verifies, only when newer than everything held of its contract, never below
// the floor.
func (c *modelClient) install(raw []byte) error {
	if !c.fresh {
		return errors.New("no fresh trust statement")
	}
	b := c.verify(raw)
	if b == nil {
		return errors.New("does not verify")
	}
	for _, h := range c.held {
		same := vcmp(h.version, b.version) == 0
		if same || (h.m.WebClientAPI == b.m.WebClientAPI && vcmp(h.version, b.version) > 0) {
			return errors.New("not newer")
		}
	}
	if vcmp(b.version, vmax(c.floor, b.floor)) < 0 {
		return errors.New("below the floor")
	}
	c.raise(b)
	c.recompute()
	c.held = append(c.held, b)
	c.applyFloor()
	c.prune()
	return nil
}

// prune keeps the built-in bundle and three cached ones per contract.
func (c *modelClient) prune() {
	keep := func(list []*modelBundle) []*modelBundle {
		n := map[int]int{}
		var out []*modelBundle
		for _, b := range list {
			if !b.builtin {
				n[b.m.WebClientAPI]++
				if n[b.m.WebClientAPI] > 3 {
					continue
				}
			}
			out = append(out, b)
		}
		return out
	}
	c.held, c.shelved = keep(c.held), keep(c.shelved)
}

// restart verifies everything held again with the keys in force; the marks
// and the statement are persisted, freshness is not.
func (c *modelClient) restart() {
	all := append(append([]*modelBundle{}, c.held...), c.shelved...)
	c.held, c.shelved, c.fresh = nil, nil, false
	c.keys = c.effectiveKeys()
	for _, old := range all {
		if b := c.verify(old.raw); b != nil {
			b.builtin = old.builtin
			c.held = append(c.held, b)
			c.raise(b)
		}
	}
	c.recompute()
	c.applyFloor()
}

// selectFor picks the newest held bundle that speaks the server's contract,
// supports its release (nil: a dev server) and is not below the high-water
// mark of its contract.
func (c *modelClient) selectFor(api int, server *Version) *modelBundle {
	for _, b := range c.held {
		if hw, ok := c.highWater[b.m.WebClientAPI]; ok && vcmp(b.version, hw) < 0 && c.mutant != "ignore the high-water mark" {
			continue
		}
		if b.m.WebClientAPI != api {
			continue
		}
		if server == nil || vcmp(*server, b.minSrv) >= 0 {
			return b
		}
	}
	return nil
}
