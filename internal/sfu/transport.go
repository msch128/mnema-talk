package sfu

import (
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/google/uuid"
	"github.com/pion/ice/v4"
)

var errSFUClosed = errors.New("SFU is closed")

type transportOptions struct {
	udpMuxPort uint16
}

// Option configures the SFU's transport at startup.
type Option func(*transportOptions)

// WithUDPMuxPort shares a UDP port across peers on each local interface.
// Zero keeps the legacy per-peer port range. A nonzero port must be inside
// that range, so existing Docker publication and router forwards cover it.
func WithUDPMuxPort(port uint16) Option {
	return func(options *transportOptions) { options.udpMuxPort = port }
}

func openUDPMux(port, portMin, portMax uint16, announceIPs []string) (ice.UDPMux, error) {
	if port == 0 {
		return nil, nil
	}
	if port < portMin || port > portMax {
		return nil, fmt.Errorf("UDP mux port %d must be inside UDP range %d-%d", port, portMin, portMax)
	}
	var options []ice.UDPMuxFromPortOption
	if onlyLoopbackAnnouncements(announceIPs) {
		options = append(options, ice.UDPMuxFromPortWithIPFilter(func(ip net.IP) bool { return ip.IsLoopback() }))
	}
	for _, ip := range announceIPs {
		if addr, err := netip.ParseAddr(ip); err == nil && addr.IsLoopback() {
			options = append(options, ice.UDPMuxFromPortWithLoopback())
			break
		}
	}
	// Pion binds separately to each usable IPv4/IPv6 interface. Its constructor
	// closes previously opened sockets if a later bind fails.
	mux, err := ice.NewMultiUDPMuxFromPort(int(port), options...)
	if err != nil {
		return nil, fmt.Errorf("listen on SFU UDP mux port: %w", err)
	}
	if len(mux.GetListenAddresses()) == 0 {
		_ = mux.Close()
		return nil, errors.New("no usable interfaces for SFU UDP mux")
	}
	return mux, nil
}

func onlyLoopbackAnnouncements(ips []string) bool {
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		addr, err := netip.ParseAddr(ip)
		if err != nil || !addr.IsLoopback() {
			return false
		}
	}
	return true
}

// Close rejects new joins and releases shared sockets before waiting for peer
// closures, so pending network writes can terminate. Call after stopping HTTP
// admission. Repeated calls are safe.
func (s *SFU) Close() error {
	s.closeOnce.Do(func() {
		s.roomsMu.Lock()
		s.closed.Store(true)
		rooms := s.rooms
		s.rooms = make(map[uuid.UUID]*Room)
		s.roomsMu.Unlock()
		if s.udpMux != nil {
			s.closeErr = errors.Join(s.closeErr, s.udpMux.Close())
		}
		for _, room := range rooms {
			room.mu.RLock()
			peers := make([]*Peer, 0, len(room.peers))
			for _, peer := range room.peers {
				peers = append(peers, peer)
			}
			room.mu.RUnlock()
			for _, peer := range peers {
				room.detachPeer(peer)
				s.closeErr = errors.Join(s.closeErr, peer.PC.Close())
			}
		}
	})
	return s.closeErr
}
