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
	options := buildUDPMuxOptions(announceIPs)
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

// udpSocketBuffer is the kernel buffer asked for on every media socket. A
// screen frame arrives as a burst of ten or more packets and a keyframe as
// 40 or more; the default (about 208 KB) overflows when the reader is briefly
// short of CPU, and the loss shows up as broken video. The kernel grants at
// most net.core.rmem_max / wmem_max (doc/deployment.md, "UDP socket buffers").
const udpSocketBuffer = 4 << 20

func buildUDPMuxOptions(announceIPs []string) []ice.UDPMuxFromPortOption {
	options := []ice.UDPMuxFromPortOption{
		ice.UDPMuxFromPortWithReadBufferSize(udpSocketBuffer),
		ice.UDPMuxFromPortWithWriteBufferSize(udpSocketBuffer),
	}
	if filter := announceIPFilter(announceIPs); filter != nil {
		options = append(options, ice.UDPMuxFromPortWithIPFilter(filter))
	}
	if announcesLoopback(announceIPs) {
		options = append(options, ice.UDPMuxFromPortWithLoopback())
	}
	return options
}

// announceIPFilter says which local addresses may become candidates when
// announced addresses are set, or nil when nothing is announced. The
// announced addresses are IPv4 only and replace every IPv4 host address, but
// Pion leaves an IPv6 address alone when no rule matches its family, so the
// container's own unique-local (Docker with IPv6) or loopback address would be
// offered to browsers unchanged. Browsers cannot reach those; the pairs only
// delay the connection. Global IPv6 addresses stay, as on a host-networked
// server that really is reachable over IPv6.
func announceIPFilter(announceIPs []string) func(net.IP) bool {
	if len(announceIPs) == 0 {
		return nil
	}
	if onlyLoopbackAnnouncements(announceIPs) {
		return func(ip net.IP) bool { return ip.IsLoopback() }
	}
	loopback := announcesLoopback(announceIPs)
	return func(ip net.IP) bool {
		if ip.To4() != nil {
			return true
		}
		if ip.IsLoopback() {
			return loopback
		}
		return ip.IsGlobalUnicast() && !ip.IsPrivate()
	}
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
			for _, peer := range room.peersSnapshot() {
				room.detachPeer(peer)
				s.closeErr = errors.Join(s.closeErr, peer.PC.Close())
			}
		}
	})
	return s.closeErr
}

func (r *Room) peersSnapshot() []*Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, peer := range r.peers {
		peers = append(peers, peer)
	}
	return peers
}

// announcesLoopback reports whether any announced address is loopback, i.e.
// the server runs where its clients can be on loopback too (dev, e2e).
func announcesLoopback(ips []string) bool {
	for _, ip := range ips {
		if addr, err := netip.ParseAddr(ip); err == nil && addr.IsLoopback() {
			return true
		}
	}
	return false
}
