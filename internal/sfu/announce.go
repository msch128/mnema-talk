package sfu

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"time"

	"github.com/pion/webrtc/v4"
)

// ResolveAnnounce turns WEBRTC_NAT_1TO1_IP entries into IPv4 addresses. An
// entry is an IP or a host name (e.g. a dynamic-DNS name for a home
// connection whose public IP changes). Duplicates are dropped, order is kept.
func ResolveAnnounce(ctx context.Context, entries []string) ([]string, error) {
	var out []string
	add := func(a netip.Addr) {
		a = a.Unmap()
		if a.Is4() && !slices.Contains(out, a.String()) {
			out = append(out, a.String())
		}
	}
	for _, e := range entries {
		if a, err := netip.ParseAddr(e); err == nil {
			if !a.Unmap().Is4() {
				return nil, fmt.Errorf("WEBRTC_NAT_1TO1_IP: %s is not an IPv4 address", e)
			}
			add(a)
			continue
		}
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", e)
		if err != nil {
			return nil, fmt.Errorf("WEBRTC_NAT_1TO1_IP: resolve %s: %w", e, err)
		}
		for _, a := range addrs {
			add(a)
		}
	}
	return out, nil
}

// announceRewriteRules turns the announced addresses into ICE address rewrite
// rules that publish them as host candidates in place of the container's own
// addresses. It mirrors what Pion derives from the deprecated
// SettingEngine.SetNAT1To1IPs(ips, ICECandidateTypeHost): all addresses
// together form one catch-all rule. Replace is the mode Pion defaults to for
// host candidates. The addresses come from ResolveAnnounce, which accepts
// plain IPs and host names only, so there is no "external/local" form.
func announceRewriteRules(ips []string) []webrtc.ICEAddressRewriteRule {
	rules := make([]webrtc.ICEAddressRewriteRule, 0, 1)
	if len(ips) > 0 {
		rules = append(rules, webrtc.ICEAddressRewriteRule{
			External:        slices.Clone(ips),
			AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode:            webrtc.ICEAddressRewriteReplace,
		})
	}
	return rules
}

// KeepAnnounceCurrent re-resolves host-name entries every interval and
// switches the SFU to the new addresses when they changed. Entries that are
// all IPs never change, so nothing runs for them.
func (s *SFU) KeepAnnounceCurrent(ctx context.Context, entries []string, every time.Duration) {
	if !hasHostName(entries) {
		return
	}
	s.announcers.Add(1)
	go func() {
		defer s.announcers.Done()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			s.refreshAnnounce(ctx, entries)
		}
	}()
}

// hasHostName reports whether any entry is a host name rather than an IP.
func hasHostName(entries []string) bool {
	for _, e := range entries {
		if _, err := netip.ParseAddr(e); err != nil {
			return true
		}
	}
	return false
}

// refreshAnnounce resolves entries once and switches the SFU to the result
// when it differs from the current addresses. A failure keeps the current ones.
func (s *SFU) refreshAnnounce(ctx context.Context, entries []string) {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	ips, err := ResolveAnnounce(rctx, entries)
	cancel()
	if err != nil {
		slog.Warn("webrtc announce: resolve failed, keeping current addresses", "err", err)
		return
	}
	if slices.Equal(ips, s.AnnouncedIPs()) {
		return
	}
	if err := s.SetAnnouncedIPs(ips); err != nil {
		slog.Warn("webrtc announce: update failed", "err", err)
		return
	}
	slog.Info("webrtc announce: addresses changed", "ips", ips)
}

// KeepAnnounceDetected repeats an address detection (a STUN probe for the
// public IP) every interval and switches the SFU to the result when it
// changed, which follows a home connection whose address is reassigned. A
// failed detection keeps the current address.
func (s *SFU) KeepAnnounceDetected(ctx context.Context, every time.Duration, detect func(context.Context) (string, error)) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			ip, err := detect(ctx)
			if err != nil {
				slog.Warn("webrtc announce: public address detection failed, keeping current addresses", "err", err)
				continue
			}
			if ips := []string{ip}; !slices.Equal(ips, s.AnnouncedIPs()) {
				if err := s.SetAnnouncedIPs(ips); err != nil {
					slog.Warn("webrtc announce: update failed", "err", err)
					continue
				}
				slog.Info("webrtc announce: detected public address changed", "ips", ips)
			}
		}
	}()
}
