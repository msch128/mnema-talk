package automod

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// recheckEvery is how often the engine asks the database whether the rules
// changed behind its back (a change made through this server reloads at once).
const recheckEvery = 30 * time.Second

// Engine evaluates the rules. The zero value is unusable; build it with New.
// All methods are safe on a nil *Engine and then do nothing, so the paths that
// use it (messages, sign-up) need no checks of their own.
type Engine struct {
	DB     *db.Pool
	Perms  *perm.Service
	Events events.Publisher
	Audit  *audit.Log

	mu      sync.RWMutex
	set     *ruleSet
	checked atomic.Int64 // unix nanoseconds of the last version check
	gen     atomic.Int64 // bumped by Invalidate, so a reload that raced it does not mark its stale rules as checked
	ages    ageCache
	now     func() time.Time
	// listed runs after a reload read the rules (tests only).
	listed func()
}

// ruleSet is one compiled version of the configuration.
type ruleSet struct {
	version int64
	rules   []*compiled // message rules, oldest first
	raid    *compiled   // nil when there is none or it is off
}

// New builds an engine; Attach makes the message and sign-up paths use it.
func New(p *db.Pool, perms *perm.Service, pub events.Publisher, log *audit.Log) *Engine {
	return &Engine{DB: p, Perms: perms, Events: pub, Audit: log, now: time.Now}
}

var registry sync.Map // *db.Pool -> *Engine

// Attach makes e the engine of the message and sign-up paths that work on p.
// The paths find it through the pool they already hold, so no signature of
// theirs changes and two servers in one process (tests) never share rules.
func Attach(p *db.Pool, e *Engine) { registry.Store(p, e) }

// Detach removes e as the engine of p, unless another one took its place.
func Detach(p *db.Pool, e *Engine) { registry.CompareAndDelete(p, e) }

// Of returns the engine attached to p, or nil.
func Of(p *db.Pool) *Engine {
	if p == nil {
		return nil
	}
	e, _ := registry.Load(p)
	en, _ := e.(*Engine)
	return en
}

// Invalidate makes the next check load the rules again.
func (e *Engine) Invalidate() {
	if e != nil {
		e.gen.Add(1)
		e.checked.Store(0)
	}
}

// current returns the compiled rules, loading them when they were never loaded
// or the version moved.
func (e *Engine) current(ctx context.Context) (*ruleSet, error) {
	e.mu.RLock()
	set := e.set
	e.mu.RUnlock()
	last := e.checked.Load()
	if set != nil && last != 0 && e.now().UnixNano()-last < int64(recheckEvery) {
		return set, nil
	}
	return e.reload(ctx, set)
}

func (e *Engine) reload(ctx context.Context, old *ruleSet) (*ruleSet, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Another caller may have reloaded while this one waited.
	if e.set != nil && e.set != old && e.checked.Load() != 0 {
		return e.set, nil
	}
	gen := e.gen.Load()
	rules, version, err := ListRules(ctx, e.DB)
	if err != nil {
		return e.set, err
	}
	if e.listed != nil {
		e.listed()
	}
	// An Invalidate that ran meanwhile may have been committed after this read:
	// leave the check mark unset so the next call loads again.
	if e.gen.Load() == gen {
		e.checked.Store(e.now().UnixNano())
	}
	if e.set != nil && e.set.version == version {
		return e.set, nil
	}
	set := &ruleSet{version: version}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		c, err := compile(r)
		if err != nil {
			slog.Error("automod rule skipped", "rule", r.ID, "err", err)
			continue
		}
		if r.Kind == KindRaid {
			set.raid = c
			continue
		}
		set.rules = append(set.rules, c)
	}
	e.set = set
	return set, nil
}

// Post is a message the author is about to send, or an edit of one.
type Post struct {
	ChannelID uuid.UUID
	UserID    uuid.UUID
	Content   string
	// HasAttachment: the message carries a file (uploads).
	HasAttachment bool
	// MessageID is the message being edited; uuid.Nil for a new one.
	MessageID uuid.UUID
}

// Pending holds the hits of a message that is let through (rules that only
// flag); Commit records them once the message is stored. The zero and nil
// values do nothing.
type Pending struct {
	e    *Engine
	post Post
	dm   bool
	hits []*compiled
}

// Check applies the message rules to a post. It returns an error (400, the
// reason in words) when a rule blocks the message; the hit is already in the
// moderation queue then. Otherwise it returns the flags to record after the
// message is stored, or nil.
func (e *Engine) Check(ctx context.Context, p Post) (*Pending, error) {
	if e == nil {
		return nil, nil
	}
	set, err := e.current(ctx)
	if err != nil {
		return nil, fmt.Errorf("automod rules: %w", err)
	}
	if len(set.rules) == 0 {
		return nil, nil
	}
	snap, err := e.Perms.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	// The Owner and Administrators are never held back; neither are bots.
	if snap.IsBot(p.UserID) || snap.Global(perm.Subject{ID: p.UserID}).Has(perm.Administrator) {
		return nil, nil
	}
	pend := &Pending{e: e, post: p, dm: snap.IsDM(p.ChannelID)}
	if err := e.evaluate(ctx, set, pend, snap.RoleIDs(p.UserID)); err != nil || len(pend.hits) == 0 {
		return nil, err
	}
	return pend, nil
}

// evaluate runs the rules in order: the first blocking hit ends it with the
// refusal, the flagging ones are collected in pend.
func (e *Engine) evaluate(ctx context.Context, set *ruleSet, pend *Pending, roles []uuid.UUID) error {
	for _, r := range set.rules {
		if pend.dm && r.Kind != KindLinks && r.Kind != KindMentions {
			continue // in a direct message only links and mention spam are checked
		}
		if exempt(r, pend.post.ChannelID, roles) || !e.hit(ctx, r, pend.post) {
			continue
		}
		if r.Action == ActionBlock {
			return e.block(ctx, pend, r)
		}
		pend.hits = append(pend.hits, r)
	}
	return nil
}

func exempt(r *compiled, channel uuid.UUID, roles []uuid.UUID) bool {
	if r.exemptChannels[channel] {
		return true
	}
	for _, id := range roles {
		if r.exemptRoles[id] {
			return true
		}
	}
	return false
}

// hit reports whether the post breaks the rule.
func (e *Engine) hit(ctx context.Context, r *compiled, p Post) bool {
	switch r.Kind {
	case KindWords:
		return r.matchesWords(p.Content)
	case KindLinks:
		return r.matchesLinks(p.Content)
	case KindMentions:
		return distinctMentions(p.Content) > r.Config.MaxMentions
	case KindNewAccount:
		return e.newAccountHit(ctx, r, p)
	}
	return false
}

func (e *Engine) newAccountHit(ctx context.Context, r *compiled, p Post) bool {
	switch r.Config.Restrict {
	case RestrictLinks:
		if len(links(p.Content)) == 0 {
			return false
		}
	case RestrictAttachments:
		if !p.HasAttachment {
			return false
		}
	case RestrictLinksAndFiles:
		if !p.HasAttachment && len(links(p.Content)) == 0 {
			return false
		}
	}
	created, err := e.ages.createdAt(ctx, e.DB, p.UserID)
	if err != nil {
		slog.Warn("automod account age", "err", err)
		return false
	}
	return e.now().Sub(created) < time.Duration(r.Config.MaxAgeSeconds)*time.Second
}

// block records the hit and returns the refusal the author sees.
func (e *Engine) block(ctx context.Context, pend *Pending, r *compiled) error {
	e.record(ctx, pend.post, pend.dm, r, "blocked", pend.post.MessageID)
	what := "Your message was not sent"
	if pend.post.MessageID != uuid.Nil {
		what = "Your edit was not saved"
	}
	return httpx.ErrInvalidInput(fmt.Sprintf("%s: it breaks the community rule %q (automod). The moderators can see this.", what, r.Name))
}

// Commit records the flags of a message that is now stored as id.
func (p *Pending) Commit(ctx context.Context, id uuid.UUID) {
	if p == nil {
		return
	}
	if p.post.MessageID != uuid.Nil {
		id = p.post.MessageID
	}
	for _, r := range p.hits {
		p.e.record(ctx, p.post, p.dm, r, "flagged", id)
	}
}
