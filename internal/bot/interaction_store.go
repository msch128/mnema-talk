package bot

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// interactionTTL is how long a bot may answer an interaction, and how
	// long an ephemeral note's components and an open modal work.
	interactionTTL = 15 * time.Minute
	// maxResponses bounds the messages one interaction can produce.
	maxResponses = 5
	// maxPending bounds the interactions kept in memory.
	maxPending = 10000
	// maxPendingPerBot and maxPendingPerInvoker keep one bot, or a few
	// accounts, from taking the whole store: the rest of the members can
	// still run the commands of other bots.
	maxPendingPerBot     = 2000
	maxPendingPerInvoker = 100
	// invokesPerMinute bounds how often one member can run commands.
	invokesPerMinute = 20
	// clicksPerMinute bounds how often one member can use components and
	// submit modals, together.
	clicksPerMinute = 30
	// flagEphemeral in a response's flags makes it visible to the invoker only.
	flagEphemeral = 64
)

// Interaction types, with Discord's numbers.
const (
	InteractionApplicationCommand = 2
	InteractionMessageComponent   = 3
	InteractionModalSubmit        = 5
)

// Callback types, with Discord's numbers (doc/bots.md, Interactions in detail).
const (
	CallbackMessage        = 4
	CallbackDeferred       = 5
	CallbackDeferredUpdate = 6
	CallbackUpdate         = 7
	CallbackModal          = 9
)

// Deliverer hands an interaction to a connected bot (the gateway).
type Deliverer interface {
	// DeliverInteraction sends INTERACTION_CREATE to one live session of the
	// bot and reports whether there was one.
	DeliverInteraction(botID uuid.UUID, d json.RawMessage) bool
}

// source is what a component interaction is about: the bot's message the
// component is on, or one of its ephemeral notes. A modal opened from a click
// passes it on to its submit.
type source struct {
	message uuid.UUID
	note    uuid.UUID
}

func (s source) set() bool { return s.message != uuid.Nil || s.note != uuid.Nil }

type interaction struct {
	id      uuid.UUID
	hash    [32]byte
	kind    int
	botID   uuid.UUID
	botUser uuid.UUID
	botName string
	avatar  string
	channel uuid.UUID
	invoker uuid.UUID
	created time.Time
	source  source

	// Guarded by the store's mutex.
	responses int
	answer    int // the first answer's callback type, 0 before it
	// laterEphemeral: the answer that follows callback 5 is ephemeral.
	laterEphemeral bool

	// op serialises the first answer and the @original routes, so the
	// original response is posted once and edited after it exists.
	op sync.Mutex
	// original is the response @original names once there is one: a message,
	// or a note when it was ephemeral. Guarded by op.
	original source
	// originalGone: @original was deleted. Guarded by op.
	originalGone bool
}

type interactionStore struct {
	mu     sync.Mutex
	m      map[uuid.UUID]*interaction
	notes  map[uuid.UUID]*note
	modals map[uuid.UUID]*modal
	now    func() time.Time
}

func newInteractionStore(now func() time.Time) *interactionStore {
	return &interactionStore{m: map[uuid.UUID]*interaction{}, notes: map[uuid.UUID]*note{}, modals: map[uuid.UUID]*modal{}, now: now}
}

func (s *interactionStore) add(id uuid.UUID, in *interaction) (token string, ok bool) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	in.hash = sha256.Sum256([]byte(token))
	in.id = id
	in.created = s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasRoom(in) {
		return "", false
	}
	s.m[id] = in
	return token, true
}

// hasRoom drops expired entries and reports whether in fits under the total
// cap and the caps of its bot and its invoker.
func (s *interactionStore) hasRoom(in *interaction) bool {
	s.prune(in.created)
	var byBot, byInvoker int
	for _, v := range s.m {
		if v.botID == in.botID {
			byBot++
		}
		if v.invoker == in.invoker {
			byInvoker++
		}
	}
	return len(s.m) < maxPending && byBot < maxPendingPerBot && byInvoker < maxPendingPerInvoker
}

// prune drops the interactions, notes and modals whose time is over.
func (s *interactionStore) prune(now time.Time) {
	for k, v := range s.m {
		if now.Sub(v.created) > interactionTTL {
			delete(s.m, k)
		}
	}
	for k, v := range s.notes {
		if now.Sub(v.created) > interactionTTL {
			delete(s.notes, k)
		}
	}
	for k, v := range s.modals {
		if now.Sub(v.created) > interactionTTL {
			delete(s.modals, k)
		}
	}
}

var errUnknownInteraction = httpx.ErrNotFound("unknown interaction")

func matchInteraction(in *interaction, botID uuid.UUID, sum [32]byte) bool {
	if in == nil || in.botID != botID {
		return false
	}
	return subtle.ConstantTimeCompare(in.hash[:], sum[:]) == 1
}

// remove drops an interaction.
func (s *interactionStore) remove(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

// find returns the interaction with this id for its bot and token, while it
// can still be answered.
func (s *interactionStore) find(id uuid.UUID, token string, botID uuid.UUID) (*interaction, error) {
	sum := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.m[id]
	if !matchInteraction(in, botID, sum) {
		return nil, errUnknownInteraction
	}
	if s.now().Sub(in.created) > interactionTTL {
		delete(s.m, id)
		return nil, errUnknownInteraction
	}
	return in, nil
}

// allowedCallback is the allowed-for table of doc/bots.md: a message (4) and
// a deferral (5) answer everything, a deferred update (6) and an update (7)
// need a component's message (a click, or a modal opened from one), a modal
// (9) answers a command or a click but not a modal submit.
func allowedCallback(in *interaction, typ int) error {
	switch typ {
	case CallbackMessage, CallbackDeferred:
		return nil
	case CallbackDeferredUpdate, CallbackUpdate:
		if in.source.set() {
			return nil
		}
		return httpx.ErrInvalidInput(fmt.Sprintf("callback type %d answers a component interaction only", typ))
	case CallbackModal:
		if in.kind != InteractionModalSubmit {
			return nil
		}
		return httpx.ErrInvalidInput("a modal submit cannot be answered with another modal")
	}
	return httpx.ErrInvalidInput("type must be 4, 5, 6, 7 or 9")
}

// takeAnswer records the first answer of type typ; a message (4) counts as
// one of the responses. undoAnswer gives it back when it could not be done.
func (s *interactionStore) takeAnswer(in *interaction, typ int, laterEphemeral bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.answer != 0 {
		return httpx.ErrConflict("the interaction already has a response")
	}
	if err := allowedCallback(in, typ); err != nil {
		return err
	}
	in.answer, in.laterEphemeral = typ, laterEphemeral
	if typ == CallbackMessage {
		in.responses++
	}
	return nil
}

func (s *interactionStore) undoAnswer(in *interaction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.answer == CallbackMessage && in.responses > 0 {
		in.responses--
	}
	in.answer, in.laterEphemeral = 0, false
}

// countResponse takes one of the responses for another message: a follow-up,
// or the first PATCH of @original after callback 5. It needs an answer that
// was not a modal. uncount gives it back when the message was not posted.
func (s *interactionStore) countResponse(in *interaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case in.answer == 0:
		return httpx.ErrConflict("respond to the interaction first")
	case in.answer == CallbackModal:
		return httpx.ErrConflict("the interaction was answered with a modal; answer its submit")
	case in.responses >= maxResponses:
		return httpx.ErrConflict(fmt.Sprintf("an interaction takes at most %d responses", maxResponses))
	}
	in.responses++
	return nil
}

func (s *interactionStore) uncount(in *interaction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.responses > 0 {
		in.responses--
	}
}

// state returns the first answer's type and whether the answer after a
// deferral is ephemeral.
func (s *interactionStore) state(in *interaction) (answer int, laterEphemeral bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return in.answer, in.laterEphemeral
}
