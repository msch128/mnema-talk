// Package turnrelay is the optional TURN relay built into the server, for
// members whose network blocks direct UDP to the SFU (corporate firewalls,
// carrier NAT, VPNs). It is a relay to the SFU and nothing else: every
// datagram it forwards goes to, or comes from, one of the SFU's own media
// sockets, so it cannot be used as an open relay or to reach another host or
// network.
//
// How the pieces fit. A browser allocates a relayed address on the relay
// (turn:host:3479), then runs ICE against the SFU through it. The relayed
// socket lives on the server itself, bound to an address the SFU's sockets
// are reachable on, so the hop from the relay to the SFU never leaves the
// machine. The address the SFU is announced under (WEBRTC_NAT_1TO1_IP, which
// may not even be routable from inside the container) is translated to the
// real local socket on the way in and back on the way out; see guard.
//
// The guard layers, outermost first:
//
//   - credentials: short-lived, signed with a secret generated at start-up,
//     naming one member (creds.go); allocating and refreshing re-checks that
//     the member may still join voice;
//   - rate limits per source address, quotas per member and in total, a byte
//     rate and an idle timeout per allocation (limit.go, guard.go);
//   - the permission handler: only the SFU's addresses may be permitted or
//     bound as peers;
//   - the relay socket itself: writes to anything but the SFU's media ports
//     are dropped, reads from anything but the SFU's sockets are dropped, so
//     even a permission slip in the handler could not open the relay;
//   - TCP allocations (RFC 6062) are refused outright.
package turnrelay
