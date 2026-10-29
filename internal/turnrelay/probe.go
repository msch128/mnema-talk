package turnrelay

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/pion/stun/v4"
)

// How soon after the start the first probe runs, and how often it repeats.
var (
	probeFirstDelay = 3 * time.Second
	probeInterval   = 10 * time.Minute
)

// probeLoop checks now and then that the relay answers on the address
// browsers are given. It is a hint only: it travels from this machine to its
// own public address, so a router without NAT loopback fails it although the
// port is forwarded, and a probe that passes through a hairpin says nothing
// about a firewall in front of the router.
func (s *Server) probeLoop(ctx context.Context) {
	defer s.wg.Done()
	timer := time.NewTimer(probeFirstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.probe.Store(s.probeOnce(ctx))
		timer.Reset(probeInterval)
	}
}

func (s *Server) probeOnce(ctx context.Context) string {
	if !s.status.UDP {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", net.JoinHostPort(s.opts.PublicHost, strconv.Itoa(s.opts.Port)))
	if err != nil {
		return "unreachable"
	}
	defer conn.Close()
	req := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(req.Raw); err != nil {
		return "unreachable"
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return "unreachable"
	}
	var resp stun.Message
	resp.Raw = buf[:n]
	if resp.Decode() != nil || resp.Type.Class != stun.ClassSuccessResponse || resp.TransactionID != req.TransactionID {
		return "unreachable"
	}
	return "reachable"
}
