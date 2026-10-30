package sfu

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

// The connection test (Probe) lets a browser find out whether its media
// reaches this server, without joining a Talk: no room, no presence, no
// audio, no media permission. It is one data channel and lives a few seconds.
const (
	// probeLifetime is the longest a test connection is kept.
	probeLifetime = 20 * time.Second
	// probeGatherTimeout bounds the wait for the answer's candidates.
	probeGatherTimeout = 3 * time.Second
	// probeLinger keeps a connected test connection open long enough for the
	// browser to read its statistics.
	probeLinger = 5 * time.Second
	// maxProbes bounds the test connections alive at once.
	maxProbes = 8
	// maxProbesPerMember keeps one member from taking every slot; it covers the
	// three paths of one test (direct, relay over UDP, relay over TCP).
	maxProbesPerMember = 3
	// maxProbeOfferBytes bounds the offer SDP a browser may send.
	maxProbeOfferBytes = 16 << 10
)

// Reasons Probe refuses an offer; the server maps them to HTTP statuses.
var (
	ErrProbeBusy    = errors.New("too many connection tests running")
	ErrProbeInvalid = errors.New("not a data-channel-only offer")
)

// Probe answers a browser's data-channel-only offer with a throwaway peer
// connection that uses the same media sockets and announced addresses as a
// Talk, so a connected test proves the path real media takes. The answer
// carries all of the server's candidates (no trickle). The connection closes
// itself after a few seconds or when it fails. Remote candidates are vetted
// like those of a Talk member, so the test cannot point the server at
// addresses it must not probe.
func (s *SFU) Probe(ctx context.Context, owner, offerSDP string) (string, error) {
	if s.closed.Load() {
		return "", errSFUClosed
	}
	api, announced := s.apiAndAnnounced()
	offer, err := vetProbeOffer(offerSDP, announcesLoopback(announced))
	if err != nil {
		return "", err
	}
	if !s.takeProbe(owner) {
		return "", ErrProbeBusy
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		s.releaseProbe(owner)
		return "", err
	}
	answer, err := answerProbe(ctx, pc, offer)
	if err != nil {
		_ = pc.Close()
		s.releaseProbe(owner)
		return "", err
	}
	s.watchProbe(pc, owner)
	return answer, nil
}

// vetProbeOffer parses the offer, refuses anything but a data channel and
// drops candidates the trickle path would refuse.
func vetProbeOffer(raw string, allowLoopback bool) (string, error) {
	if len(raw) == 0 || len(raw) > maxProbeOfferBytes {
		return "", ErrProbeInvalid
	}
	var desc sdp.SessionDescription
	if err := desc.Unmarshal([]byte(raw)); err != nil {
		return "", fmt.Errorf("%w: %v", ErrProbeInvalid, err)
	}
	if len(desc.MediaDescriptions) != 1 || desc.MediaDescriptions[0].MediaName.Media != "application" {
		return "", ErrProbeInvalid
	}
	media := desc.MediaDescriptions[0]
	media.Attributes = keepProbeCandidates(media.Attributes, allowLoopback)
	out, err := desc.Marshal()
	return string(out), err
}

// keepProbeCandidates drops the candidates vetCandidateKey refuses and every
// one beyond maxRemoteCandidates.
func keepProbeCandidates(attrs []sdp.Attribute, allowLoopback bool) []sdp.Attribute {
	budget := maxRemoteCandidates
	kept := attrs[:0]
	for _, a := range attrs {
		if a.Key == "candidate" {
			if _, err := vetCandidateKey(a.Value, allowLoopback); err != nil || budget == 0 {
				continue
			}
			budget--
		}
		kept = append(kept, a)
	}
	return kept
}

func answerProbe(ctx context.Context, pc *webrtc.PeerConnection, offer string) (string, error) {
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return "", fmt.Errorf("%w: %v", ErrProbeInvalid, err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return "", err
	}
	select {
	case <-gathered:
	case <-time.After(probeGatherTimeout):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return pc.LocalDescription().SDP, nil
}

// watchProbe logs the outcome, tells the ICE observer, and closes the
// connection when it ends or its time is up.
func (s *SFU) watchProbe(pc *webrtc.PeerConnection, owner string) {
	var once atomic.Bool
	closeProbe := func() {
		if once.CompareAndSwap(false, true) {
			s.releaseProbe(owner)
			_ = pc.Close()
		}
	}
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		switch state {
		case webrtc.ICEConnectionStateConnected:
			pair, _ := connectionPair(pc)
			if pair != nil {
				slog.Info("sfu probe ice connected", "remote", pair.Remote.Address, "remote_type", pair.Remote.Typ.String())
			}
			s.iceSeen.connected(pair)
			time.AfterFunc(probeLinger, closeProbe)
		case webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateClosed:
			closeProbe()
		}
	})
	// The browser closes its end once it has read its statistics; the slot
	// is free again then, not after the linger.
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			closeProbe()
		}
	})
	time.AfterFunc(probeLifetime, closeProbe)
}

// takeProbe reserves a slot for owner, within the global and per-member caps.
func (s *SFU) takeProbe(owner string) bool {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if s.probes >= maxProbes || s.probesBy[owner] >= maxProbesPerMember {
		return false
	}
	if s.probesBy == nil {
		s.probesBy = make(map[string]int)
	}
	s.probes++
	s.probesBy[owner]++
	return true
}

func (s *SFU) releaseProbe(owner string) {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	s.probes--
	if s.probesBy[owner]--; s.probesBy[owner] <= 0 {
		delete(s.probesBy, owner)
	}
}

// IsClosed reports whether err says the SFU has been shut down.
func IsClosed(err error) bool { return errors.Is(err, errSFUClosed) }
