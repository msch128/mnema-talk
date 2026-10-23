package ws

import "encoding/binary"

// The handshake channel carries three kinds of messages (web/src/lib/media):
// MLS messages (RFC 9420 MLSMessage: a u16 protocol version, then a u16 wire
// format) and two fixed-size signed messages beginning with a two-byte magic.
// The server reads only these few header bytes, never the content: it cannot
// tell a genuine message from a forged one (members vet that, end to end), but
// it can refuse what is not shaped like any of them, so an arbitrary blob
// cannot be fanned out to the room for every member to decode and verify.
const (
	mlsVersion10 = 1

	mlsPublicMessage = 1
	mlsWelcome       = 3
	mlsKeyPackage    = 5

	heartbeatBytes  = 78 // 'M' 'H', epoch u32, time f64, Ed25519 signature
	capabilityBytes = 67 // 'M' 'C', bits u8, Ed25519 signature
)

// wellFormedHandshake reports whether raw has the framing of a key package,
// welcome or public message (a commit), a heartbeat or a capability message.
// Private messages and group infos are never sent in this protocol.
func wellFormedHandshake(raw []byte) bool {
	if len(raw) >= 2 && raw[0] == 'M' {
		switch raw[1] {
		case 'H':
			return len(raw) == heartbeatBytes
		case 'C':
			return len(raw) == capabilityBytes
		}
		return false
	}
	if len(raw) < 4 || binary.BigEndian.Uint16(raw) != mlsVersion10 {
		return false
	}
	switch binary.BigEndian.Uint16(raw[2:]) {
	case mlsPublicMessage, mlsWelcome, mlsKeyPackage:
		return true
	}
	return false
}
