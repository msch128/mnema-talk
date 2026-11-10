package sfu

import (
	"bytes"
	"testing"
	"time"

	"github.com/pion/rtcp"
)

// FuzzScanRTCP checks the header scan of a compound RTCP batch and the
// decoders it feeds: nothing panics, the packets handed out are the batch in
// order without gaps or overlap, and what is left over is a cut-off packet.
func FuzzScanRTCP(f *testing.F) {
	twcc, rr := twccFeedback(f), receiverReportBytes(f)
	batch, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: 1, Reports: []rtcp.ReceptionReport{{SSRC: 2, FractionLost: 12, LastSenderReport: 7, Delay: 3}}},
		&rtcp.TransportLayerNack{SenderSSRC: 1, MediaSSRC: 2, Nacks: []rtcp.NackPair{{PacketID: 1, LostPackets: 0x8001}}},
		&rtcp.PictureLossIndication{SenderSSRC: 1, MediaSSRC: 2},
		&rtcp.FullIntraRequest{FIR: []rtcp.FIREntry{{SSRC: 2}}},
	})
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		nil,
		{0x8f, 205},
		rr,
		twcc,
		batch,
		append(bytes.Clone(rr), twcc...),
		append(bytes.Clone(rr), 0x8f, 205, 0xff, 0xff, 0),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, compound []byte) {
		rest := compound
		feedback := 0
		scanRTCP(compound, func(typ byte, format uint8, raw []byte) {
			if len(raw) < 4 || len(raw)%4 != 0 || len(raw) > len(rest) {
				t.Fatalf("packet of %d bytes with %d left", len(raw), len(rest))
			}
			if &raw[0] != &rest[0] {
				t.Fatal("packet does not start where the previous one ended")
			}
			if typ != raw[1] || format != raw[0]&0x1f {
				t.Fatalf("type %d format %d for header % x", typ, format, raw[:4])
			}
			if typ == rtcpTypeTransportFeedback && format == fmtTransportCC {
				feedback++
			}
			rest = rest[len(raw):]
		})
		if len(rest) >= 4 && 4*(1+(int(rest[2])<<8|int(rest[3]))) <= len(rest) {
			t.Fatalf("scan stopped before a complete packet: % x", rest)
		}
		got := transportFeedbackOf(compound)
		if len(got) > feedback {
			t.Fatalf("%d feedback packets decoded out of %d in the batch", len(got), feedback)
		}
		for _, fb := range got {
			if fb == nil {
				t.Fatal("nil feedback packet")
			}
		}
		// The other readers of a viewer's batch: the feedback counters and the
		// round trip time the retransmission responder keeps.
		countFeedback(compound)
		n := &nackResponder{
			now:     func() time.Time { return time.Unix(1_700_000_000, 0) },
			streams: map[uint32]*nackStream{2: {}},
		}
		scanRTCP(compound, func(typ byte, _ uint8, raw []byte) {
			if typ == rtcpTypeReceiverReport {
				n.noteRTT(raw)
			}
		})
	})
}
