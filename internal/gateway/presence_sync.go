package gateway

import "github.com/google/uuid"

// syncPresence reports the bots whose online state changed since the last
// call. A bot's status and activity outlive its connection for the resume
// window (as in Discord, a RESUME keeps them): they are dropped once the bot
// has no session left, and reported again when the bot comes back.
func (g *Gateway) syncPresence() {
	if g.OnPresence == nil {
		return
	}
	// pcb orders the callbacks against setPresence's, so a status set while
	// the bot leaves cannot reach OnStatus after OnPresence(false).
	g.pcb.Lock()
	defer g.pcb.Unlock()
	g.mu.Lock()
	live, has := g.onlineBotsLocked()
	changes := g.presenceChangesLocked(live, has)
	// What a returning bot had set before it detached.
	back := map[uuid.UUID]Presence{}
	for _, id := range changes {
		if p, ok := g.pres[id]; ok && live[id] {
			back[id] = p
		}
	}
	g.announced = live
	g.mu.Unlock()
	for _, id := range changes {
		g.OnPresence(id, live[id])
		if p, ok := back[id]; ok && g.OnStatus != nil {
			g.OnStatus(id, p.Status, p.Activity)
		}
	}
}

// onlineBotsLocked returns the bots with a live session, and those with any
// session at all. Callers hold g.mu.
func (g *Gateway) onlineBotsLocked() (live, has map[uuid.UUID]bool) {
	live = map[uuid.UUID]bool{}
	has = map[uuid.UUID]bool{}
	for _, s := range g.sessions {
		has[s.userID] = true
		if s.live() {
			live[s.userID] = true
		}
	}
	return live, has
}

// presenceChangesLocked lists the bots whose announced state differs from
// live, and forgets the status of bots without any session. Callers hold g.mu.
func (g *Gateway) presenceChangesLocked(live, has map[uuid.UUID]bool) []uuid.UUID {
	var changes []uuid.UUID
	for id := range live {
		if !g.announced[id] {
			changes = append(changes, id)
		}
	}
	for id := range g.announced {
		if !live[id] {
			changes = append(changes, id)
		}
	}
	// A status ends with the last session; one set by a bot that left before
	// it was ever announced online is reported once so the hub drops it.
	for id := range g.pres {
		if !has[id] {
			delete(g.pres, id)
			if !g.announced[id] && !live[id] {
				changes = append(changes, id)
			}
		}
	}
	return changes
}
