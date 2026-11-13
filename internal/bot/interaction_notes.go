package bot

import (
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// Ephemeral notes and open modals live in the interaction store, in memory,
// for the 15 minutes of the interaction that made them; a restart forgets
// them (doc/bots.md, Message components and Modals).
const (
	// maxNotes and maxNotesPerBot bound the remembered notes. A note past the
	// cap is still shown; its components then answer 404.
	maxNotes       = 5000
	maxNotesPerBot = 1000
)

// note is an ephemeral response as the member sees it: never stored, and only
// remembered while its components or its @original may still be used.
type note struct {
	id         uuid.UUID
	botID      uuid.UUID
	botUser    uuid.UUID
	channel    uuid.UUID
	invoker    uuid.UUID
	created    time.Time // of the interaction it answers
	content    string
	embeds     []core.Embed
	components []core.ActionRow
}

// modal is a form a bot opened for one member, which they may submit once.
type modal struct {
	id      uuid.UUID
	botID   uuid.UUID
	botUser uuid.UUID
	botName string
	avatar  string
	channel uuid.UUID
	invoker uuid.UUID
	created time.Time
	form    core.Modal
	// source is the component's message or note when a click opened it.
	source source
	used   bool
}

// keepNote remembers n (a copy) and reports whether there was room.
func (s *interactionStore) keepNote(n note) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.notes[n.id]; !ok {
		s.prune(s.now())
		byBot := 0
		for _, v := range s.notes {
			if v.botID == n.botID {
				byBot++
			}
		}
		if len(s.notes) >= maxNotes || byBot >= maxNotesPerBot {
			return false
		}
	}
	s.notes[n.id] = &n
	return true
}

// noteFor returns a copy of the note id of member in channel while it lives.
func (s *interactionStore) noteFor(id, member, channel uuid.UUID) (note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.notes[id]
	if n == nil || n.invoker != member || n.channel != channel || s.now().Sub(n.created) > interactionTTL {
		return note{}, false
	}
	return *n, true
}

// noteOf returns a copy of the bot's note id while it lives.
func (s *interactionStore) noteOf(id, botID uuid.UUID) (note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.notes[id]
	if n == nil || n.botID != botID || s.now().Sub(n.created) > interactionTTL {
		return note{}, false
	}
	return *n, true
}

// dropNote forgets a note (deleted by its bot).
func (s *interactionStore) dropNote(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.notes, id)
}

// openModal remembers m for its member.
func (s *interactionStore) openModal(m *modal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.created = s.now()
	s.modals[m.id] = m
}

// takeModal hands the modal id to member in channel once, within its 15
// minutes; giveBackModal makes it usable again when the submit could not be
// delivered.
func (s *interactionStore) takeModal(id, member, channel uuid.UUID) (*modal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.modals[id]
	if m == nil || m.used || m.invoker != member || m.channel != channel || s.now().Sub(m.created) > interactionTTL {
		return nil, false
	}
	m.used = true
	return m, true
}

func (s *interactionStore) giveBackModal(m *modal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.used = false
}

// closeModal forgets a submitted modal.
func (s *interactionStore) closeModal(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.modals, id)
}
