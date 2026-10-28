package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// stopTimers ends the countdowns of a test's manager, so none fires into the
// next test's database.
func (m *TalkManager) stopTimers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.talks {
		if t.timer != nil {
			t.timer.Stop()
		}
	}
}

func newBareTalkManager() *TalkManager {
	return NewTalkManager(&TalkManager{EmptyGrace: time.Hour})
}

func TestAFailedRemovalCountsTheEmptyTalkDownAgain(t *testing.T) {
	m := newBareTalkManager()
	ch := uuid.New()
	talk := &liveTalk{}
	m.talks[ch] = talk
	m.finishRemoval(ch, talk, errors.New("database down"))
	defer m.stopTimers()
	if m.talks[ch] != talk || talk.timer == nil {
		t.Fatal("a Talk that could not be removed must stay registered with a running countdown")
	}
}

func TestAJoinDuringAFailedRemovalKeepsTheTalkAlive(t *testing.T) {
	m := newBareTalkManager()
	ch, user := uuid.New(), uuid.New()
	other := uuid.New()
	talk := &liveTalk{leader: &other}
	m.talks[ch] = talk
	// The Talk stays registered while it is removed, so the join is seen.
	m.OnPresence(user, ch, true)
	if !m.applyNext() {
		t.Fatal("the join of a Talk under removal was dropped")
	}
	m.finishRemoval(ch, talk, errors.New("database down"))
	if len(talk.members) != 1 || talk.timer != nil {
		t.Fatalf("members = %v, timer = %v; an occupied Talk must not be counted down", talk.members, talk.timer)
	}
}

func TestARemovedTalkIsForgotten(t *testing.T) {
	m := newBareTalkManager()
	ch := uuid.New()
	talk := &liveTalk{}
	m.talks[ch] = talk
	m.finishRemoval(ch, talk, nil)
	if m.talks[ch] != nil {
		t.Fatal("a removed Talk is still registered")
	}
}

func TestAStaleTalkThatCouldNotBeSweptIsRetried(t *testing.T) {
	m := newBareTalkManager()
	ch := uuid.New()
	m.retryRemoval(ch)
	defer m.stopTimers()
	if m.talks[ch] == nil || m.talks[ch].timer == nil {
		t.Fatal("the leftover Talk has no countdown, so nothing retries its removal")
	}
}
