package mediaframe

import (
	"bytes"
	"testing"
)

// FuzzWellFormedHandshake checks the framing guard never panics and accepts
// nothing but the five message shapes of the protocol: the two signed
// messages at their exact size, and an MLS 1.0 key package, welcome or public
// message of any length.
func FuzzWellFormedHandshake(f *testing.F) {
	for _, seed := range [][]byte{
		mlsWire(mlsKeyPackage, 100), mlsWire(mlsWelcome, 100), mlsWire(mlsPublicMessage, 100),
		sizedWire('H', heartbeatBytes), sizedWire('C', capabilityBytes),
		mlsWire(2, 100), mlsWire(4, 100), mlsWire(0, 100), mlsWire(9, 100),
		{0, 2, 0, mlsKeyPackage, 1, 2}, {0, 1, 0}, nil,
		sizedWire('H', heartbeatBytes-1), sizedWire('H', heartbeatBytes+1), sizedWire('C', capabilityBytes-1),
		sizedWire('X', 80), []byte("hello world"), {'M'},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		ok := WellFormedHandshake(raw)
		longer := WellFormedHandshake(append(bytes.Clone(raw), 0))
		if !ok {
			return
		}
		switch {
		case bytes.HasPrefix(raw, []byte("MH")):
			if len(raw) != heartbeatBytes || longer {
				t.Fatalf("heartbeat of %d bytes accepted (one byte more: %v)", len(raw), longer)
			}
		case bytes.HasPrefix(raw, []byte("MC")):
			if len(raw) != capabilityBytes || longer {
				t.Fatalf("capability message of %d bytes accepted (one byte more: %v)", len(raw), longer)
			}
		case bytes.HasPrefix(raw, []byte{0, mlsVersion10, 0}) && len(raw) >= 4:
			if format := raw[3]; format != mlsPublicMessage && format != mlsWelcome && format != mlsKeyPackage {
				t.Fatalf("MLS wire format %d accepted", format)
			}
			if !longer {
				t.Fatal("an MLS message is refused once it is a byte longer")
			}
		default:
			t.Fatalf("accepted % x", raw)
		}
	})
}
