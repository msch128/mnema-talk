package mediaframe

import "testing"

// mlsWire is a handshake with the framing of an MLSMessage of the given wire
// format, followed by n opaque bytes.
func mlsWire(format byte, n int) []byte {
	return append([]byte{0, mlsVersion10, 0, format}, make([]byte, n)...)
}

// sizedWire is a heartbeat or capability message ('M' + kind) of n bytes.
func sizedWire(kind byte, n int) []byte {
	b := make([]byte, n)
	b[0], b[1] = 'M', kind
	return b
}

func TestWellFormedHandshakeAcceptsOnlyTheProtocolsMessages(t *testing.T) {
	for name, tc := range map[string]struct {
		wire []byte
		ok   bool
	}{
		"key package":            {mlsWire(mlsKeyPackage, 100), true},
		"welcome":                {mlsWire(mlsWelcome, 100), true},
		"public message":         {mlsWire(mlsPublicMessage, 100), true},
		"heartbeat":              {sizedWire('H', heartbeatBytes), true},
		"capability":             {sizedWire('C', capabilityBytes), true},
		"private message":        {mlsWire(2, 100), false},
		"group info":             {mlsWire(4, 100), false},
		"reserved wire format":   {mlsWire(0, 100), false},
		"unknown wire format":    {mlsWire(9, 100), false},
		"other protocol version": {append([]byte{0, 2, 0, mlsKeyPackage}, 1, 2), false},
		"truncated header":       {[]byte{0, 1, 0}, false},
		"empty":                  {nil, false},
		"short heartbeat":        {sizedWire('H', heartbeatBytes-1), false},
		"long heartbeat":         {sizedWire('H', heartbeatBytes+1), false},
		"short capability":       {sizedWire('C', capabilityBytes-1), false},
		"unknown magic":          {sizedWire('X', 80), false},
		"text":                   {[]byte("hello world"), false},
	} {
		if got := WellFormedHandshake(tc.wire); got != tc.ok {
			t.Errorf("%s: WellFormedHandshake = %v, want %v", name, got, tc.ok)
		}
	}
}
