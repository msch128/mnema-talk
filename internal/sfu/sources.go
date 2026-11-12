package sfu

import (
	"time"

	"github.com/google/uuid"
)

// Source tells what a forwarded track carries. Audio and screen tracks are
// published under the user's ID as stream ID; a camera under CameraStreamPrefix
// and a screen share's sound under ScreenAudioStreamPrefix plus the user's ID,
// so clients can tell them apart and map them to people.
type Source string

const (
	SourceAudio  Source = "audio"
	SourceScreen Source = "screen"
	SourceCamera Source = "camera"
	// SourceScreenAudio is the sound of a screen share. It travels apart from
	// the voice, so viewers set its volume on its own, and like the shared
	// screen it only goes to the viewers who watch.
	SourceScreenAudio Source = "screen-audio"
)

// SourceSet is a set of media sources.
type SourceSet uint8

func sourceBit(s Source) SourceSet {
	switch s {
	case SourceAudio:
		return 1
	case SourceScreen:
		return 2
	case SourceCamera:
		return 4
	case SourceScreenAudio:
		return 8
	}
	return 0
}

// With returns the set plus s.
func (set SourceSet) With(s Source) SourceSet { return set | sourceBit(s) }

// Has reports whether s is in the set.
func (set SourceSet) Has(s Source) bool { return set&sourceBit(s) != 0 }

// SetDeniedSources sets which sources the peer's user may not publish
// (without the Speak or Video permission): their packets are dropped, and a
// track that arrives for one is ignored.
func (p *Peer) SetDeniedSources(set SourceSet) { p.denied.Store(uint32(set)) }

// DeniedSources is the set given to SetDeniedSources.
func (p *Peer) DeniedSources() SourceSet { return SourceSet(p.denied.Load()) }

func (p *Peer) sourceDenied(s Source) bool { return SourceSet(p.denied.Load()).Has(s) }

// takeViewerTrack removes and returns the outgoing layered track trackID.
func (p *Peer) takeViewerTrack(trackID string) *viewerTrack {
	p.vtMu.Lock()
	defer p.vtMu.Unlock()
	vt := p.viewerTracks[trackID]
	delete(p.viewerTracks, trackID)
	return vt
}

// putViewerTrack records the outgoing layered track trackID. It reports false,
// recording nothing, once the peer's tracks were released: a signaling round
// still running then must not register a viewer nobody will release.
func (p *Peer) putViewerTrack(trackID string, vt *viewerTrack) bool {
	p.vtMu.Lock()
	defer p.vtMu.Unlock()
	if p.vtReleased {
		return false
	}
	if p.viewerTracks == nil {
		p.viewerTracks = make(map[string]*viewerTrack)
	}
	p.viewerTracks[trackID] = vt
	return true
}

// releaseViewerTracks stops every layered track this peer receives.
func (p *Peer) releaseViewerTracks() {
	p.vtMu.Lock()
	tracks := p.viewerTracks
	p.viewerTracks = nil
	p.vtReleased = true
	p.vtMu.Unlock()
	for _, vt := range tracks {
		vt.group.removeViewer(vt)
	}
}

func matchesSource(tInfoSource Source, source Source) bool {
	if tInfoSource == source {
		return true
	}
	return source == SourceScreen && tInfoSource == SourceScreenAudio
}

// stopSourceTracksLocked stops the publisher's tracks of source; one stop
// spends one resume of the source's budget, however many tracks (layers, the
// share's sound) it stops. r.mu is held.
func (r *Room) stopSourceTracksLocked(userID uuid.UUID, source Source) bool {
	var resumeAt int64
	for id, tInfo := range r.trackLocals {
		if tInfo.SenderID == userID && matchesSource(tInfo.Source, source) {
			if resumeAt == 0 {
				resumeAt = r.stopUntilLocked(userID, source, layerNow())
			}
			tInfo.resumeAt.Store(resumeAt)
			delete(r.trackLocals, id)
		}
	}
	return resumeAt != 0
}

// sourceKey names one source of one publisher.
type sourceKey struct {
	user   uuid.UUID
	source Source
}

// resumeBudget is how many resumes a publisher's source has left (resumeBurst
// at most, refilled one per resumeRefill) as of at. It belongs to the
// publisher and source, not to a track, so a replaced capture or a rejoin does
// not start a fresh burst.
type resumeBudget struct {
	left float64
	at   time.Time
}

// leftAt is what the budget has left at now.
func (b *resumeBudget) leftAt(now time.Time) float64 {
	return min(resumeBurst, b.left+float64(now.Sub(b.at))/float64(resumeRefill))
}

// stopUntilLocked spends one resume of the source's budget for a stop at now
// and returns when its tracks may resume (steadyNanos): after resumeHoldoff
// while the budget lasts, else once it has refilled by one. The budgets of
// publishers no longer in the room are forgotten once full again, so the map
// holds at most one entry per source of each member seated now or within the
// last (resumeBurst+1)*resumeRefill (110 s). r.mu is held.
func (r *Room) stopUntilLocked(user uuid.UUID, source Source, now time.Time) int64 {
	for k, b := range r.resumeBudgets {
		if r.peers[k.user] == nil && b.leftAt(now) >= resumeBurst {
			delete(r.resumeBudgets, k)
		}
	}
	if r.resumeBudgets == nil {
		r.resumeBudgets = map[sourceKey]*resumeBudget{}
	}
	key := sourceKey{user, source}
	b := r.resumeBudgets[key]
	if b == nil {
		b = &resumeBudget{left: resumeBurst, at: now}
		r.resumeBudgets[key] = b
	}
	// Clamped at -1: a stop never waits longer than resumeRefill.
	b.left, b.at = max(b.leftAt(now)-1, -1), now
	wait := resumeHoldoff
	if b.left < 0 {
		wait = max(wait, time.Duration(-b.left*float64(resumeRefill)))
	}
	return steadyNanos(now.Add(wait))
}
