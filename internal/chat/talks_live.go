package chat

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/events"
)

// apply folds one join or leave into the live state.
func (m *TalkManager) apply(ev presenceEvent) {
	m.mu.Lock()
	t := m.talks[ev.channel]
	if t == nil {
		m.mu.Unlock()
		return
	}
	var change leaderChange
	if ev.joined {
		change = m.joinLocked(t, ev.user)
	} else {
		change = m.leaveLocked(ev.channel, t, ev.user)
	}
	m.mu.Unlock()
	if change.changed {
		m.persistLeader(ev.channel, change.leader)
	}
}

type leaderChange struct {
	changed bool
	leader  *uuid.UUID
}

// joinLocked seats user; a Talk without a leader is led by its first member.
func (m *TalkManager) joinLocked(t *liveTalk, user uuid.UUID) leaderChange {
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if !slices.Contains(t.members, user) {
		t.members = append(t.members, user)
	}
	if t.leader == nil {
		t.leader = &user
		return leaderChange{true, t.leader}
	}
	return leaderChange{}
}

// leaveLocked removes user. When the leader goes, the member who has been in
// the Talk longest takes over. An empty Talk starts its removal countdown.
func (m *TalkManager) leaveLocked(ch uuid.UUID, t *liveTalk, user uuid.UUID) leaderChange {
	t.members = slices.DeleteFunc(t.members, func(id uuid.UUID) bool { return id == user })
	var change leaderChange
	if t.leader != nil && *t.leader == user {
		t.leader = nil
		if len(t.members) > 0 {
			next := t.members[0]
			t.leader = &next
		}
		change = leaderChange{true, t.leader}
	}
	if len(t.members) == 0 {
		m.armLocked(ch, t)
	}
	return change
}

// persistLeader stores a new leader and tells everyone who can see the Talk.
func (m *TalkManager) persistLeader(ch uuid.UUID, leader *uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := SetTalkLeader(ctx, m.DB, ch, leader); err != nil {
		slog.Warn("talks: store leader", "channel", ch, "err", err)
	}
	m.Events.BroadcastChannel(ch, events.TalkLeader, map[string]any{"channel_id": ch, "leader_id": leader})
}

// armLocked starts (or restarts) the countdown after which an empty Talk is
// removed.
func (m *TalkManager) armLocked(ch uuid.UUID, t *liveTalk) {
	if t.timer != nil {
		t.timer.Stop()
	}
	t.timer = time.AfterFunc(m.EmptyGrace, func() { m.removeIfEmpty(ch) })
}

// removeIfEmpty deletes the temporary Talk ch unless someone is in it.
func (m *TalkManager) removeIfEmpty(ch uuid.UUID) {
	m.mu.Lock()
	t := m.talks[ch]
	if t == nil || len(t.members) > 0 {
		m.mu.Unlock()
		return
	}
	delete(m.talks, ch)
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.remove(ctx, ch, "empty"); err != nil {
		slog.Warn("talks: remove empty Talk", "channel", ch, "err", err)
		m.mu.Lock()
		m.talks[ch] = t
		m.armLocked(ch, t)
		m.mu.Unlock()
	}
}

// remove deletes a temporary Talk and records that the system did it.
func (m *TalkManager) remove(ctx context.Context, ch uuid.UUID, reason string) error {
	var name string
	_ = m.DB.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1 AND is_temp`, ch).Scan(&name)
	if name == "" {
		return nil // already gone, or not temporary: nothing to remove
	}
	keys, err := DeleteChannel(ctx, m.DB, ch)
	if err != nil {
		return err
	}
	if m.Voice != nil {
		m.Voice.CloseVoiceChannel(ch)
	}
	if m.Objects != nil && len(keys) > 0 {
		if err := m.Objects.DeleteBatch(ctx, keys); err != nil {
			slog.Warn("talks: delete media objects", "count", len(keys), "err", err)
		}
	}
	m.Audit.RecordOrLog(ctx, audit.Entry{
		ActorName: "system", Action: "channel.delete", TargetType: "channel",
		TargetID: ch.String(), TargetName: name,
		Details: map[string]any{"temporary": true, "reason": reason},
	})
	m.structureChanged()
	return nil
}

// sweepStale removes every temporary Talk left from before a restart.
func (m *TalkManager) sweepStale(ctx context.Context) {
	if m.DB == nil {
		return
	}
	rows, err := m.DB.Query(ctx, `SELECT id FROM channels WHERE is_temp`)
	if err != nil {
		slog.Warn("talks: list stale Talks", "err", err)
		return
	}
	var stale []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			stale = append(stale, id)
		}
	}
	rows.Close()
	for _, id := range stale {
		if err := m.remove(ctx, id, "restart"); err != nil {
			slog.Warn("talks: remove stale Talk", "channel", id, "err", err)
		}
	}
}
