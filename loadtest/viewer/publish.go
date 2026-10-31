package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// pubRole says what a member publishes. The SFU tells the lines of its offer
// apart by their position: audio line 0 is the microphone, video line 1 the
// screen, video line 2 the camera.
type pubRole struct {
	screen, camera, audio bool
}

func (r pubRole) any() bool { return r.screen || r.camera || r.audio }

func (r pubRole) String() string {
	switch {
	case r.screen:
		return "screen"
	case r.camera:
		return "camera"
	case r.audio:
		return "audio"
	}
	return "viewer"
}

// publishSettings are the synthetic streams' shapes.
type publishSettings struct {
	screenKbps, screenFPS int
	cameraKbps, cameraFPS int
	keySeconds            int
}

func loadPublishSettings() publishSettings {
	return publishSettings{
		screenKbps: envInt("SCREEN_KBPS", 1500), screenFPS: envInt("SCREEN_FPS", 15),
		cameraKbps: envInt("CAMERA_KBPS", 300), cameraFPS: envInt("CAMERA_FPS", 24),
		keySeconds: envInt("KEYFRAME_S", 3),
	}
}

// Positions of the SFU's receiving lines in its offer.
const (
	lineMic    = 0
	lineScreen = 1
	lineCamera = 2
)

// startPublishing attaches the member's synthetic tracks to the SFU's
// receiving lines, once, after the first offer was applied and before the
// answer is created. The generators start sending when the connection is up.
func (m *member) startPublishing(pc *webrtc.PeerConnection) error {
	if !m.role.any() {
		return nil
	}
	var err error
	m.pubOnce.Do(func() { err = m.attachTracks(pc) })
	return err
}

func (m *member) attachTracks(pc *webrtc.PeerConnection) error {
	lines := pc.GetTransceivers()
	if len(lines) <= lineCamera {
		return errors.New("the offer has no receiving lines to publish on")
	}
	set := loadPublishSettings()
	if m.role.audio {
		if err := m.publishAudio(lines[lineMic]); err != nil {
			return err
		}
	}
	if m.role.screen {
		if err := m.publishVideo(lines[lineScreen], set.screenKbps, set.screenFPS, set.keySeconds, "screen"); err != nil {
			return err
		}
	}
	if m.role.camera {
		return m.publishVideo(lines[lineCamera], set.cameraKbps, set.cameraFPS, set.keySeconds, "camera")
	}
	return nil
}

// attach puts a local track on a receiving line of the offer.
func (m *member) attach(line *webrtc.RTPTransceiver, track *webrtc.TrackLocalStaticRTP) (*webrtc.RTPSender, error) {
	sender, err := m.api.NewRTPSender(track, line.Receiver().Transport())
	if err != nil {
		return nil, err
	}
	if err := line.SetSender(sender, track); err != nil {
		return nil, err
	}
	return sender, nil
}

func (m *member) publishVideo(line *webrtc.RTPTransceiver, kbps, fps, keySeconds int, name string) error {
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: videoClock},
		name, m.id)
	if err != nil {
		return err
	}
	sender, err := m.attach(line, track)
	if err != nil {
		return fmt.Errorf("publish %s: %w", name, err)
	}
	src := newVP8Source(kbps, fps, keySeconds)
	go m.readFeedback(sender, src)
	go m.sendVideo(track, src)
	return nil
}

func (m *member) publishAudio(line *webrtc.RTPTransceiver) error {
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: opusClock, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"},
		"audio", m.id)
	if err != nil {
		return err
	}
	if _, err := m.attach(line, track); err != nil {
		return fmt.Errorf("publish audio: %w", err)
	}
	go m.sendAudio(track)
	return nil
}

// readFeedback reads the SFU's RTCP for a published video track: a keyframe
// request makes the next frame a keyframe, as a browser's encoder does. Reading
// is also what runs Pion's interceptors for the sender.
func (m *member) readFeedback(sender *webrtc.RTPSender, src *vp8Source) {
	for {
		pkts, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, p := range pkts {
			switch p.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				src.requestKeyframe()
			}
		}
	}
}

// paceTicks calls fn on every tick of interval until the member stops. The
// ticks are paced against a fixed schedule, so a slow tick is made up for.
func (m *member) paceTicks(interval time.Duration, fn func()) {
	next := time.Now()
	for {
		next = next.Add(interval)
		select {
		case <-m.stop:
			return
		case <-time.After(time.Until(next)):
		}
		if m.connected.Load() {
			fn()
		}
		if time.Since(next) > time.Second {
			next = time.Now() // far behind: do not burst to catch up
		}
	}
}

func (m *member) sendVideo(track *webrtc.TrackLocalStaticRTP, src *vp8Source) {
	m.paceTicks(src.frameInterval(), func() {
		for _, p := range src.nextFrame() {
			if track.WriteRTP(p) == nil {
				m.sentB.Add(int64(len(p.Payload)))
				m.sentPkt.Add(1)
			}
		}
	})
}

func (m *member) sendAudio(track *webrtc.TrackLocalStaticRTP) {
	src := newOpusSource()
	m.paceTicks(opusFrameMs*time.Millisecond, func() {
		p := src.next()
		if track.WriteRTP(p) == nil {
			m.sentB.Add(int64(len(p.Payload)))
			m.sentPkt.Add(1)
		}
	})
}
