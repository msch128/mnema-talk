package bot

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func addInteraction(t *testing.T, s *interactionStore, in *interaction) (*interaction, string) {
	t.Helper()
	id := uuid.New()
	token, ok := s.add(id, in)
	if !ok {
		t.Fatal("add")
	}
	found, err := s.find(id, token, in.botID)
	if err != nil {
		t.Fatal(err)
	}
	return found, token
}

func TestAnInteractionTakesOneFirstAnswerAndAFailedOneCanBeRetried(t *testing.T) {
	s := newInteractionStore(time.Now)
	in, _ := addInteraction(t, s, &interaction{botID: uuid.New(), kind: InteractionApplicationCommand})
	if err := s.takeAnswer(in, CallbackMessage, false); err != nil {
		t.Fatal(err)
	}
	if err := s.takeAnswer(in, CallbackMessage, false); err == nil {
		t.Fatal("a second first answer was taken")
	}
	s.undoAnswer(in)
	if err := s.takeAnswer(in, CallbackDeferred, true); err != nil {
		t.Fatalf("after undo: %v", err)
	}
	if answer, later := s.state(in); answer != CallbackDeferred || !later {
		t.Fatalf("state = %d %v", answer, later)
	}
	// A deferral is no message: all five messages are still to come, and a
	// failed one does not use one up.
	for i := 0; i < maxResponses*3; i++ {
		if err := s.countResponse(in); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		s.uncount(in)
	}
	for i := 0; i < maxResponses; i++ {
		if err := s.countResponse(in); err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
	}
	if err := s.countResponse(in); err == nil {
		t.Fatal("a sixth message was allowed")
	}
}

func TestFollowUpsNeedAnAnswerThatIsNotAModal(t *testing.T) {
	s := newInteractionStore(time.Now)
	in, _ := addInteraction(t, s, &interaction{botID: uuid.New(), kind: InteractionApplicationCommand})
	if err := s.countResponse(in); err == nil {
		t.Fatal("a follow-up before the first answer")
	}
	if err := s.takeAnswer(in, CallbackModal, false); err != nil {
		t.Fatal(err)
	}
	if err := s.countResponse(in); err == nil {
		t.Fatal("a follow-up after a modal")
	}
}

func TestCallbackTypesFollowTheAllowedForTable(t *testing.T) {
	msg := source{message: uuid.New()}
	cases := []struct {
		name string
		in   *interaction
		ok   map[int]bool
	}{
		{"command", &interaction{kind: InteractionApplicationCommand}, map[int]bool{4: true, 5: true, 9: true}},
		{"click", &interaction{kind: InteractionMessageComponent, source: msg}, map[int]bool{4: true, 5: true, 6: true, 7: true, 9: true}},
		{"submit of a command's modal", &interaction{kind: InteractionModalSubmit}, map[int]bool{4: true, 5: true}},
		{"submit of a click's modal", &interaction{kind: InteractionModalSubmit, source: msg}, map[int]bool{4: true, 5: true, 6: true, 7: true}},
	}
	for _, tc := range cases {
		for _, typ := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
			err := allowedCallback(tc.in, typ)
			if (err == nil) != tc.ok[typ] {
				t.Errorf("%s, callback %d: err = %v", tc.name, typ, err)
			}
		}
	}
}

func TestInteractionRemove(t *testing.T) {
	s := newInteractionStore(time.Now)
	id := uuid.New()
	s.add(id, &interaction{botID: uuid.New()})
	s.remove(id)
	if len(s.m) != 0 {
		t.Fatal("remove keeps the interaction")
	}
}

func TestAnInteractionIsUnknownToAnotherBotAWrongTokenAndAfterItsTime(t *testing.T) {
	now := time.Now()
	s := newInteractionStore(func() time.Time { return now })
	bot := uuid.New()
	id := uuid.New()
	token, _ := s.add(id, &interaction{botID: bot})
	if _, err := s.find(id, token, uuid.New()); err == nil {
		t.Fatal("another bot found it")
	}
	if _, err := s.find(id, token+"x", bot); err == nil {
		t.Fatal("a wrong token found it")
	}
	now = now.Add(interactionTTL + time.Second)
	if _, err := s.find(id, token, bot); err == nil {
		t.Fatal("found after its time")
	}
}

func TestANoteIsOnlyTheInvokersAndLivesAsLongAsItsInteraction(t *testing.T) {
	now := time.Now()
	s := newInteractionStore(func() time.Time { return now })
	member, ch, bot := uuid.New(), uuid.New(), uuid.New()
	n := note{id: uuid.New(), botID: bot, invoker: member, channel: ch, created: now, content: "hi"}
	if !s.keepNote(n) {
		t.Fatal("keep")
	}
	if _, ok := s.noteFor(n.id, uuid.New(), ch); ok {
		t.Fatal("another member got the note")
	}
	if _, ok := s.noteFor(n.id, member, uuid.New()); ok {
		t.Fatal("the note was found in another channel")
	}
	if _, ok := s.noteOf(n.id, uuid.New()); ok {
		t.Fatal("another bot got the note")
	}
	if got, ok := s.noteFor(n.id, member, ch); !ok || got.content != "hi" {
		t.Fatalf("own note: %v %v", got, ok)
	}
	now = now.Add(interactionTTL + time.Second)
	if _, ok := s.noteFor(n.id, member, ch); ok {
		t.Fatal("the note outlived its interaction")
	}
}

func TestNotesPastTheCapOfABotAreNotRemembered(t *testing.T) {
	s := newInteractionStore(time.Now)
	bot := uuid.New()
	for i := 0; i < maxNotesPerBot; i++ {
		if !s.keepNote(note{id: uuid.New(), botID: bot, created: time.Now()}) {
			t.Fatalf("refused at %d", i)
		}
	}
	if s.keepNote(note{id: uuid.New(), botID: bot, created: time.Now()}) {
		t.Fatal("a note past the bot's cap was kept")
	}
	if !s.keepNote(note{id: uuid.New(), botID: uuid.New(), created: time.Now()}) {
		t.Fatal("another bot's note was refused")
	}
}

func TestAModalIsSubmittedOnceByItsMemberWithinItsTime(t *testing.T) {
	now := time.Now()
	s := newInteractionStore(func() time.Time { return now })
	member, ch := uuid.New(), uuid.New()
	m := &modal{id: uuid.New(), invoker: member, channel: ch}
	s.openModal(m)
	if _, ok := s.takeModal(m.id, uuid.New(), ch); ok {
		t.Fatal("another member took the modal")
	}
	got, ok := s.takeModal(m.id, member, ch)
	if !ok {
		t.Fatal("its member could not take it")
	}
	if _, ok := s.takeModal(m.id, member, ch); ok {
		t.Fatal("taken twice")
	}
	s.giveBackModal(got)
	if _, ok := s.takeModal(m.id, member, ch); !ok {
		t.Fatal("not usable after it was given back")
	}
	s.closeModal(m.id)
	if _, ok := s.takeModal(m.id, member, ch); ok {
		t.Fatal("a closed modal was taken")
	}
	late := &modal{id: uuid.New(), invoker: member, channel: ch}
	s.openModal(late)
	now = now.Add(interactionTTL + time.Second)
	if _, ok := s.takeModal(late.id, member, ch); ok {
		t.Fatal("taken after its time")
	}
}

func TestCommandColumnsAs(t *testing.T) {
	want := "c.id, c.bot_id, c.name, c.description, c.options, c.disabled, c.created_at, c.default_member_permissions"
	if got := commandColumnsAs("c"); got != want {
		t.Fatalf("got %q", got)
	}
}
