package sfu

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"
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
// SettingEngine.SetNAT1To1IPs(ips, ICECandidateTypeHost): an "external/local"
// entry also gets a rule pinned to that local address, and all external
// addresses together form one catch-all rule. Replace is the mode Pion
// defaults to for host candidates.
func announceRewriteRules(ips []string) []webrtc.ICEAddressRewriteRule {
	catchAll := make([]string, 0, len(ips))
	rules := make([]webrtc.ICEAddressRewriteRule, 0, len(ips)+1)
	for _, ip := range ips {
		external, local, pinned := strings.Cut(ip, "/")
		if pinned {
			rules = append(rules, webrtc.ICEAddressRewriteRule{
				External:        []string{external},
				Local:           local,
				AsCandidateType: webrtc.ICECandidateTypeHost,
				Mode:            webrtc.ICEAddressRewriteReplace,
			})
		}
		catchAll = append(catchAll, external)
	}
	if len(catchAll) > 0 {
		rules = append(rules, webrtc.ICEAddressRewriteRule{
			External:        catchAll,
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
	dynamic := false
	for _, e := range entries {
		if _, err := netip.ParseAddr(e); err != nil {
			dynamic = true
		}
	}
	if !dynamic {
		return
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			ips, err := ResolveAnnounce(rctx, entries)
			cancel()
			if err != nil {
				slog.Warn("webrtc announce: resolve failed, keeping current addresses", "err", err)
				continue
			}
			if slices.Equal(ips, s.AnnouncedIPs()) {
				continue
			}
			if err := s.SetAnnouncedIPs(ips); err != nil {
				slog.Warn("webrtc announce: update failed", "err", err)
				continue
			}
			slog.Info("webrtc announce: addresses changed", "ips", ips)
		}
	}()
}
