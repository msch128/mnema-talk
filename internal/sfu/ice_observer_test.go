package sfu

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

func TestICEReportsDisconnectedAfterRealClientTransportCloses(t *testing.T) {
	logs := captureSFULog(t)
	s := newTestSFU(t)
	// The client's media socket is one the test owns, so the test can take the
	// network away from under a live connection.
	socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = socket.Close() })
	publisher := newClientWith(t, func(settings *webrtc.SettingEngine) {
		settings.SetICEUDPMux(ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: socket}))
	})
	publisher.publishAudio("disconnect-microphone")
	publisher.join(s, uuid.New())
	waitSFUCondition(t, "connected publisher ICE transport", func() bool {
		return publisher.peer.PC.ICEConnectionState() == webrtc.ICEConnectionStateConnected
	})
	// Closing the client's PeerConnection would send a DTLS close_notify, and
	// Pion then closes the SFU's PeerConnection at once (the peer is removed
	// without an ICE disconnect). A lost network sends nothing: closing the
	// socket makes the consent checks fail. Pion's default ICE disconnection
	// timeout is five seconds; no synthetic ICE callbacks or modified
	// transport settings are involved.
	if err := socket.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(9 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatalf("closed client did not produce an ICE disconnected warning; state=%s", publisher.peer.PC.ICEConnectionState())
		case <-poll.C:
			if strings.Contains(logs.String(), "msg=\"sfu ice disconnected\"") {
				if publisher.peer.PC.ICEConnectionState() != webrtc.ICEConnectionStateDisconnected {
					t.Fatalf("disconnect warning disagrees with ICE state: %s", publisher.peer.PC.ICEConnectionState())
				}
				return
			}
		}
	}
}
