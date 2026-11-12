package sfu

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/safego"
	"github.com/pion/ice/v4"
	"github.com/pion/transport/v5/packetio"
	"github.com/pion/webrtc/v4"
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
		s.stopUploadGovernor()
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

// The buffers of a connection's SRTP and SRTCP streams (see rtcpStreamCap).
// Pion's default for SRTCP is 100 KB: a stream that is read (feedback on a
// track the SFU sends) holds a few packets at a time, and one that nobody
// reads keeps all it got.
const (
	rtcpBufferPackets = 128
	rtcpBufferBytes   = 16 << 10
	// rtpBufferBytes is Pion's own limit of an SRTP stream (srtpBufferSize).
	rtpBufferBytes = 1000 * 1000
)

// srtpBuffer is the SettingEngine's BufferFactory: Pion's own buffer (srtp's
// packetBuffer, which keeps each packet's attributes) with the limits above.
func srtpBuffer(kind packetio.BufferPacketType, _ uint32) io.ReadWriteCloser {
	b := packetio.NewBuffer()
	if kind == packetio.RTCPBufferPacket {
		b.SetLimitCount(rtcpBufferPackets)
		b.SetLimitSize(rtcpBufferBytes)
	} else {
		b.SetLimitSize(rtpBufferBytes)
	}
	return attrBuffer{b}
}

type attrBuffer struct{ *packetio.Buffer }

func (b attrBuffer) Read(p []byte) (int, error) {
	n, _, err := b.Buffer.Read(p, nil)
	return n, err
}

func (b attrBuffer) Write(p []byte) (int, error) { return b.Buffer.Write(p, nil) }

func (b attrBuffer) ReadWithAttributes(p []byte, a packetio.Attributes) (int, packetio.Attributes, error) {
	return b.Buffer.Read(p, a)
}

func (b attrBuffer) WriteWithAttributes(p []byte, a packetio.Attributes) (int, error) {
	return b.Buffer.Write(p, a)
}

// rtcpBuffer is a member connection's SRTCP buffer: it tells the cap when its
// stream closes (Pion may close a stream twice).
type rtcpBuffer struct {
	attrBuffer
	limit *rtcpStreamCap
	ssrc  uint32
	done  atomic.Bool
}

func (b *rtcpBuffer) Close() error {
	if b.done.CompareAndSwap(false, true) {
		b.limit.closed(b.ssrc)
	}
	return b.attrBuffer.Close()
}

// quietBuffer takes every packet and keeps none; a read waits until it is
// closed. The stream of an SSRC nothing on the SFU reads costs no buffer, and
// the rest of its compound packet still reaches the other streams.
type quietBuffer struct {
	once sync.Once
	done chan struct{}
}

func newQuietBuffer() *quietBuffer { return &quietBuffer{done: make(chan struct{})} }

func (b *quietBuffer) Read([]byte) (int, error) {
	<-b.done
	return 0, io.EOF
}

func (b *quietBuffer) Write(p []byte) (int, error) { return len(p), nil }

func (b *quietBuffer) Close() error {
	b.once.Do(func() { close(b.done) })
	return nil
}

// closedBuffer holds nothing and takes nothing: what Pion makes after the cap
// closed the connection.
type closedBuffer struct{}

func (closedBuffer) Read([]byte) (int, error)  { return 0, io.EOF }
func (closedBuffer) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (closedBuffer) Close() error              { return nil }

// sentTrack records the SSRCs of a track the SFU now sends on the connection,
// as long as there are fewer than buffered streams may be open, and closes
// the connection past maxSentTracks senders.
func (c *rtcpStreamCap) sentTrack(sender *webrtc.RTPSender) {
	if c == nil {
		return
	}
	encodings := sender.GetParameters().Encodings
	c.mu.Lock()
	for _, e := range encodings {
		if len(c.sent) < maxBufferedRTCPStreams {
			c.sent[uint32(e.SSRC)] = true
		}
		if e.RTX.SSRC != 0 && len(c.sent) < maxBufferedRTCPStreams {
			c.sent[uint32(e.RTX.SSRC)] = false
		}
	}
	c.senders++
	senders, over := c.senders, c.senders > c.senderCap
	c.mu.Unlock()
	if over {
		c.trip("senders", senders)
	}
}

// stoppedTrack retires the SSRCs of a track the SFU no longer sends.
func (c *rtcpStreamCap) stoppedTrack(sender *webrtc.RTPSender) {
	if c == nil {
		return
	}
	encodings := sender.GetParameters().Encodings
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range encodings {
		c.retire(uint32(e.SSRC))
		if e.RTX.SSRC != 0 {
			c.retire(uint32(e.RTX.SSRC))
		}
	}
}

// retire requires c.mu.
func (c *rtcpStreamCap) retire(ssrc uint32) {
	delete(c.sent, ssrc)
	if _, ok := c.retired[ssrc]; ok {
		return
	}
	delete(c.retired, c.retiredRing[c.retiredNext])
	c.retiredRing[c.retiredNext] = ssrc
	c.retired[ssrc] = struct{}{}
	c.retiredNext = (c.retiredNext + 1) % rtcpRetiredSSRCs
}

// trip closes the connection once, on a goroutine of its own: Pion calls the
// cap from the loops its SRTCP session waits for while closing. The room then
// reports the peer lost (closedPeer), so the hub frees the seat or, after the
// sender cap, renews the member's connection.
func (c *rtcpStreamCap) trip(count string, n int) {
	pc := c.pc.Load()
	if pc == nil || !c.closing.CompareAndSwap(false, true) {
		return
	}
	if count == "senders" {
		c.renew.Store(true)
		c.log.Info("sfu connection made too many senders, renewing it", count, n)
		metrics.SFUIngressDropped.With("senders").Inc()
	} else {
		c.log.Warn("sfu connection keeps state for too many SSRCs, closing it", count, n)
		metrics.SFUIngressDropped.With("rtcp_ssrcs").Inc()
	}
	safego.Go("sfu_rtcp_cap", safego.Abandon, func() { _ = pc.Close() })
}

// tripped reports whether a cap closed the connection.
func (c *rtcpStreamCap) tripped() bool { return c != nil && c.closing.Load() }
