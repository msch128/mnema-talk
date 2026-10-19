package sfu

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

func (p *Peer) putViewerTrack(trackID string, vt *viewerTrack) {
	p.vtMu.Lock()
	defer p.vtMu.Unlock()
	if p.viewerTracks == nil {
		p.viewerTracks = make(map[string]*viewerTrack)
	}
	p.viewerTracks[trackID] = vt
}

// releaseViewerTracks stops every layered track this peer receives.
func (p *Peer) releaseViewerTracks() {
	p.vtMu.Lock()
	tracks := p.viewerTracks
	p.viewerTracks = nil
	p.vtMu.Unlock()
	for _, vt := range tracks {
		vt.group.removeViewer(vt)
	}
}
