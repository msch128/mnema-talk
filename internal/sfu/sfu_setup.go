package sfu

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

// SetMediaStateHandler registers the callback that announces when a user
// starts or stops publishing a screen share or camera.
func (s *SFU) SetMediaStateHandler(fn func(roomID, userID uuid.UUID, state MediaState)) {
	s.notifier.set(fn)
}

// SetScreenViewersHandler registers the callback that announces who watches
// a screen share: the sorted, distinct users in the room subscribed to
// sharer's live share. It runs only when that set changes, with an empty
// list once a watched share ends or loses its last viewer.
func (s *SFU) SetScreenViewersHandler(fn func(roomID, sharer uuid.UUID, viewers []uuid.UUID)) {
	s.notifier.setViewers(fn)
}

// SetScreenLayersHandler registers the callback that announces the layers of
// a live screen share, once and again whenever they change; an empty list
// once the share ends.
func (s *SFU) SetScreenLayersHandler(fn func(roomID, sharer uuid.UUID, layers []Layer)) {
	s.notifier.setLayers(fn)
}

// SetScreenQualityHandler registers the callback that tells a viewer which
// layer of a screen share it receives, whenever that changes.
func (s *SFU) SetScreenQualityHandler(fn func(roomID, viewer, sharer uuid.UUID, state QualityState)) {
	s.notifier.setQuality(fn)
}

// SetLayersWantedHandler registers the callback that tells a sharer which of
// its layers some viewer needs, so it can stop encoding the others.
func (s *SFU) SetLayersWantedHandler(fn func(roomID, sharer uuid.UUID, layers []int)) {
	s.notifier.setWanted(fn)
}

// NewSFU creates the SFU. announceIPs are the addresses browsers are told to
// send media to instead of the container's own (WEBRTC_NAT_1TO1_IP): usually
// the public IP and the LAN IP, so both remote and local members connect.
// stunURLs is optional: with announced IPs the server needs no STUN, and
// leaving it empty avoids contacting third parties.
func NewSFU(portMin, portMax uint16, announceIPs []string, stunURLs []string, options ...Option) (*SFU, error) {
	opts := transportOptions{}
	for _, option := range options {
		option(&opts)
	}
	mux, err := openUDPMux(opts.udpMuxPort, portMin, portMax, announceIPs)
	if err != nil {
		return nil, err
	}
	load := &peerLoad{}
	api, err := buildAPI(portMin, portMax, announceIPs, mux, load)
	if err != nil {
		if mux != nil {
			_ = mux.Close()
		}
		return nil, err
	}

	var iceServers []webrtc.ICEServer
	if len(stunURLs) > 0 {
		iceServers = []webrtc.ICEServer{{URLs: stunURLs}}
	}

	return &SFU{
		api:          api,
		load:         load,
		announced:    append([]string(nil), announceIPs...),
		portMin:      portMin,
		portMax:      portMax,
		udpMux:       mux,
		iceServers:   iceServers,
		offerTimeout: defaultOfferTimeout,
		rooms:        make(map[uuid.UUID]*Room),
		notifier:     &mediaNotifier{},
		iceSeen:      &iceObserver{},
	}, nil
}

// AnnouncedIPs returns the addresses currently offered to browsers.
func (s *SFU) AnnouncedIPs() []string {
	s.apiMu.RLock()
	defer s.apiMu.RUnlock()
	return append([]string(nil), s.announced...)
}

// SetAnnouncedIPs switches to new announced addresses (e.g. after the public
// IP of a home connection changed). Connections made from now on use them;
// running ones keep theirs.
func (s *SFU) SetAnnouncedIPs(ips []string) error {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if s.closed.Load() {
		return errSFUClosed
	}
	if s.udpMux != nil && onlyLoopbackAnnouncements(s.announced) != onlyLoopbackAnnouncements(ips) {
		return fmt.Errorf("changing UDP mux loopback interface mode requires restart")
	}
	api, err := buildAPI(s.portMin, s.portMax, ips, s.udpMux, s.load)
	if err != nil {
		return err
	}
	s.api = api
	s.announced = append([]string(nil), ips...)
	return nil
}

func (s *SFU) currentAPI() *webrtc.API {
	s.apiMu.RLock()
	defer s.apiMu.RUnlock()
	return s.api
}

// apiAndAnnounced returns the API with the announced addresses it was built
// for, read together so a concurrent SetAnnouncedIPs cannot split them.
func (s *SFU) apiAndAnnounced() (*webrtc.API, []string) {
	s.apiMu.RLock()
	defer s.apiMu.RUnlock()
	return s.api, append([]string(nil), s.announced...)
}

// buildSettingEngine configures ICE: loopback handling, the UDP mux or port
// range, and the announced addresses.
func buildSettingEngine(portMin, portMax uint16, announceIPs []string, mux ice.UDPMux) (webrtc.SettingEngine, error) {
	settingEngine := webrtc.SettingEngine{}
	if err := configureICEAddresses(&settingEngine, announceIPs); err != nil {
		return settingEngine, err
	}
	if err := configureICEPorts(&settingEngine, portMin, portMax, mux); err != nil {
		return settingEngine, err
	}
	return settingEngine, nil
}

func configureICEAddresses(se *webrtc.SettingEngine, announceIPs []string) error {
	if filter := announceIPFilter(announceIPs); filter != nil {
		se.SetIPFilter(filter)
	}
	if announcesLoopback(announceIPs) {
		se.SetIncludeLoopbackCandidate(true)
	}
	if len(announceIPs) > 0 {
		if err := se.SetICEAddressRewriteRules(announceRewriteRules(announceIPs)...); err != nil {
			return fmt.Errorf("failed to set announced addresses: %w", err)
		}
	}
	return nil
}

func configureICEPorts(se *webrtc.SettingEngine, portMin, portMax uint16, mux ice.UDPMux) error {
	if mux != nil {
		se.SetICEUDPMux(mux)
		return nil
	}
	if portMin > 0 && portMax > 0 {
		if err := se.SetEphemeralUDPPortRange(portMin, portMax); err != nil {
			return fmt.Errorf("failed to set UDP port range: %w", err)
		}
	}
	return nil
}

func buildAPI(portMin, portMax uint16, announceIPs []string, mux ice.UDPMux, load *peerLoad) (*webrtc.API, error) {
	settingEngine, err := buildSettingEngine(portMin, portMax, announceIPs, mux)
	if err != nil {
		return nil, err
	}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register default WebRTC codecs: %w", err)
	}

	// NACK (retransmissions both ways), RTCP sender/receiver reports and
	// transport-wide congestion control feedback to the publishers.
	registry := &interceptor.Registry{}
	// The bandwidth estimator is innermost, and the transport-cc stamp it
	// reads is just outside it (beneath the authorization gate, so a refused
	// packet never takes a sequence number the browser would report as lost).
	registry.Add(bweFactory{})
	if err := webrtc.ConfigureTWCCHeaderExtensionSender(mediaEngine, registry); err != nil {
		return nil, fmt.Errorf("failed to register the transport-cc stamp: %w", err)
	}
	registry.Add(telemetryFactory{})
	// Pion's NACK responder keeps a history of a fixed size per connection;
	// it is given no streams and ours (nack_responder.go) answers instead.
	options := []webrtc.InterceptorOption{webrtc.WithNackResponderOptions(disablePionResponder())}
	if err := webrtc.RegisterDefaultInterceptorsWithOptions(mediaEngine, registry, options...); err != nil {
		return nil, fmt.Errorf("failed to register WebRTC interceptors: %w", err)
	}
	registry.Add(nackResponderFactory{load: load})

	return webrtc.NewAPI(
		webrtc.WithSettingEngine(settingEngine),
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(registry),
	), nil
}
