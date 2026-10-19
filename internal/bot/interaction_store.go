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
	_, _ = rand.Read(raw)
	token = base64.RawURLEncoding.EncodeToString(raw)
	in.hash = sha256.Sum256([]byte(token))
	in.created = s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.m) >= maxPending {
		for k, v := range s.m {
			if in.created.Sub(v.created) > interactionTTL {
				delete(s.m, k)
			}
		}
		if len(s.m) >= maxPending {
			return "", false
		}
	}
	s.m[id] = in
	return token, true
}

var errUnknownInteraction = httpx.ErrNotFound("unknown interaction")

// claim finds an interaction for its bot and token and counts one response.
func (s *interactionStore) claim(id uuid.UUID, token string, botID uuid.UUID, callback bool) (*interaction, error) {
	sum := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.m[id]
	if in == nil || in.botID != botID || subtle.ConstantTimeCompare(in.hash[:], sum[:]) != 1 {
		return nil, errUnknownInteraction
	}
	if s.now().Sub(in.created) > interactionTTL {
		delete(s.m, id)
		return nil, errUnknownInteraction
	}
	if callback && in.called {
		return nil, httpx.ErrConflict("the interaction already has a response")
	}
	if !callback && !in.called {
		return nil, httpx.ErrConflict("respond to the interaction first")
	}
	if in.responses >= maxResponses {
		return nil, httpx.ErrConflict(fmt.Sprintf("an interaction takes at most %d responses", maxResponses))
	}
	if callback {
		in.called = true
	}
	in.responses++
	return in, nil
}
