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
	// interactionTTL is how long a bot may answer an interaction.
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
	// flagEphemeral in a response's flags makes it visible to the invoker only.
	flagEphemeral = 64
)

// Interaction types, with Discord's numbers.
const (
	InteractionApplicationCommand = 2
	// Callback types.
	CallbackMessage  = 4
	CallbackDeferred = 5
)

// Deliverer hands an interaction to a connected bot (the gateway).
type Deliverer interface {
	// DeliverInteraction sends INTERACTION_CREATE to one live session of the
	// bot and reports whether there was one.
	DeliverInteraction(botID uuid.UUID, d json.RawMessage) bool
}

type interaction struct {
	id        uuid.UUID
	hash      [32]byte
	botID     uuid.UUID
	botUser   uuid.UUID
	botName   string
	avatar    string
	channel   uuid.UUID
	invoker   uuid.UUID
	created   time.Time
	responses int
	called    bool
}

type interactionStore struct {
	mu  sync.Mutex
	m   map[uuid.UUID]*interaction
	now func() time.Time
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
	var byBot, byInvoker int
	for k, v := range s.m {
		if in.created.Sub(v.created) > interactionTTL {
			delete(s.m, k)
			continue
		}
		if v.botID == in.botID {
			byBot++
		}
		if v.invoker == in.invoker {
			byInvoker++
		}
	}
	return len(s.m) < maxPending && byBot < maxPendingPerBot && byInvoker < maxPendingPerInvoker
}

var errUnknownInteraction = httpx.ErrNotFound("unknown interaction")

func matchInteraction(in *interaction, botID uuid.UUID, sum [32]byte) bool {
	if in == nil || in.botID != botID {
		return false
	}
	return subtle.ConstantTimeCompare(in.hash[:], sum[:]) == 1
}

func validateInteractionState(in *interaction, callback bool) error {
	switch {
	case callback && in.called:
		return httpx.ErrConflict("the interaction already has a response")
	case !callback && !in.called:
		return httpx.ErrConflict("respond to the interaction first")
	case in.responses >= maxResponses:
		return httpx.ErrConflict(fmt.Sprintf("an interaction takes at most %d responses", maxResponses))
	default:
		return nil
	}
}

// remove drops an interaction.
func (s *interactionStore) remove(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

// release gives back the response slot claim took, for a response that could
// not be posted, so the bot can try again.
func (s *interactionStore) release(id uuid.UUID, callback bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.m[id]
	if in == nil {
		return
	}
	if callback {
		in.called = false
	}
	if in.responses > 0 {
		in.responses--
	}
}

// claim finds an interaction for its bot and token and counts one response.
func (s *interactionStore) claim(id uuid.UUID, token string, botID uuid.UUID, callback bool) (*interaction, error) {
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
	if err := validateInteractionState(in, callback); err != nil {
		return nil, err
	}
	if callback {
		in.called = true
	}
	in.responses++
	return in, nil
}
