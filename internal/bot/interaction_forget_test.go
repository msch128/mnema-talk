package bot

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestForgettingABotDropsItsInteractionsNotesAndModalsOnly(t *testing.T) {
	s := newInteractionStore(time.Now)
	gone, kept := uuid.New(), uuid.New()
	member, ch := uuid.New(), uuid.New()
	goneID, goneToken := uuid.New(), ""
	var ok bool
	if goneToken, ok = s.add(goneID, &interaction{botID: gone, invoker: member}); !ok {
		t.Fatal("add")
	}
	_, keptToken := addInteraction(t, s, &interaction{botID: kept, invoker: member})
	keptID := uuid.Nil
	for id, in := range s.m {
		if in.botID == kept {
			keptID = id
		}
	}
	goneNote, keptNote := note{id: uuid.New(), botID: gone, invoker: member, channel: ch, created: time.Now()}, note{id: uuid.New(), botID: kept, invoker: member, channel: ch, created: time.Now()}
	s.keepNote(goneNote)
	s.keepNote(keptNote)
	goneModal, keptModal := &modal{id: uuid.New(), botID: gone, invoker: member, channel: ch}, &modal{id: uuid.New(), botID: kept, invoker: member, channel: ch}
	s.openModal(goneModal)
	s.openModal(keptModal)

	s.forgetBot(gone)

	if _, err := s.find(goneID, goneToken, gone); err == nil {
		t.Fatal("the forgotten bot can still answer its interaction")
	}
	if _, ok := s.noteFor(goneNote.id, member, ch); ok {
		t.Fatal("the forgotten bot's note is still usable")
	}
	if _, ok := s.takeModal(goneModal.id, member, ch); ok {
		t.Fatal("the forgotten bot's modal can still be submitted")
	}
	if _, err := s.find(keptID, keptToken, kept); err != nil {
		t.Fatalf("another bot's interaction was dropped: %v", err)
	}
	if _, ok := s.noteFor(keptNote.id, member, ch); !ok {
		t.Fatal("another bot's note was dropped")
	}
	if _, ok := s.takeModal(keptModal.id, member, ch); !ok {
		t.Fatal("another bot's modal was dropped")
	}
}
