package sfu

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestAnnounceDetectedFollowsTheDetectedAddressAndSurvivesFailure(t *testing.T) {
	s, err := NewSFU(0, 0, []string{"198.51.100.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var step atomic.Int32
	s.KeepAnnounceDetected(ctx, 5*time.Millisecond, nil, func(context.Context) (string, error) {
		switch step.Load() {
		case 0:
			return "198.51.100.1", nil
		case 1:
			return "198.51.100.9", nil
		}
		return "", errors.New("stun unreachable")
	})
	time.Sleep(30 * time.Millisecond)
	if !slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.1"}) {
		t.Fatalf("unchanged detection moved the address: %v", s.AnnouncedIPs())
	}
	step.Store(1)
	waitSFUCondition(t, "new detected address", func() bool {
		return slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.9"})
	})
	step.Store(2)
	time.Sleep(30 * time.Millisecond)
	if !slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.9"}) {
		t.Fatalf("failed detection discarded the working address: %v", s.AnnouncedIPs())
	}
}

func TestAnnounceDetectedKeepsTheLANAddressBehindTheNewPublicOne(t *testing.T) {
	s, err := NewSFU(0, 0, []string{"198.51.100.1", "192.168.1.20"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.KeepAnnounceDetected(ctx, 5*time.Millisecond, []string{"192.168.1.20"}, func(context.Context) (string, error) {
		return "198.51.100.9", nil
	})
	waitSFUCondition(t, "public address replaced, LAN address kept", func() bool {
		return slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.9", "192.168.1.20"})
	})
}
