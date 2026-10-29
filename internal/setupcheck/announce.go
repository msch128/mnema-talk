package setupcheck

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/stunprobe"
)

// Detector finds the public IPv4 address (stunprobe.Detect in production).
type Detector func(ctx context.Context, servers []string, perServer time.Duration) (netip.Addr, string, error)

// Announcement is the voice address decision made at startup.
type Announcement struct {
	// Entries are the WEBRTC_NAT_1TO1_IP style entries handed to the SFU:
	// IPs or host names. Empty keeps the container's own address.
	Entries []string
	// Source says where they came from (SourceEnv, SourcePublicURL, SourceDetected or SourceNone).
	Source string
	// DetectErr is why the automatic detection found nothing; "" when it
	// was not tried or succeeded.
	DetectErr string
}

// detectTimeout bounds the wait for each STUN server at startup.
const detectTimeout = 2 * time.Second

// ResolveAnnouncement decides which address browsers send media to. An
// explicit WEBRTC_NAT_1TO1_IP always wins and is left alone. Only a
// production-like server without one looks further: an IPv4 address in
// PUBLIC_URL is announced as it is, and, unless WEBRTC_AUTO_ANNOUNCE=false,
// the public address is found with a STUN request and added.
func ResolveAnnouncement(ctx context.Context, cfg *config.Config, detect Detector) Announcement {
	if len(cfg.WebRTCAnnounce) > 0 {
		return Announcement{Entries: cfg.WebRTCAnnounce, Source: SourceEnv}
	}
	if !cfg.IsProduction() {
		return Announcement{Source: SourceNone}
	}
	a := Announcement{Source: SourceNone}
	if ip, ok := cfg.PublicIPv4(); ok {
		a.Entries, a.Source = []string{ip.String()}, SourcePublicURL
		if !ip.IsPrivate() {
			return a
		}
	}
	if !cfg.WebRTCAutoAnnounce {
		return a
	}
	ip, via, err := detect(ctx, cfg.WebRTCDetectSTUN, detectTimeout)
	if err != nil {
		a.DetectErr = err.Error()
		slog.Warn("webrtc announce: could not detect the public address", "err", err)
		return a
	}
	slog.Info("webrtc announce: detected the public address", "ip", ip.String(), "stun", via)
	if s := ip.String(); !slices.Contains(a.Entries, s) {
		a.Entries = append(a.Entries, s)
	}
	a.Source = SourceDetected
	return a
}

// DetectPublicIP is the Detector the server runs periodically after startup.
func DetectPublicIP(ctx context.Context, cfg *config.Config) (string, error) {
	ip, _, err := stunprobe.Detect(ctx, cfg.WebRTCDetectSTUN, detectTimeout)
	if err != nil {
		return "", err
	}
	return ip.String(), nil
}
