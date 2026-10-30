// Package stunprobe finds the server's public IPv4 address with a STUN
// binding request (RFC 5389): the one packet exchange needed to learn the
// address the internet sees, without any STUN library.
package stunprobe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// DefaultServers are public STUN servers asked when nothing else is
// configured. They only ever see one binding request from this host.
var DefaultServers = []string{"stun.l.google.com:19302", "stun.cloudflare.com:3478"}

// DefaultTimeout bounds the wait for each server.
const DefaultTimeout = 2 * time.Second

const (
	magicCookie        = 0x2112A442
	headerLen          = 20
	typeBindingRequest = 0x0001
	typeBindingSuccess = 0x0101
	attrMappedAddress  = 0x0001
	attrXORMapped      = 0x0020
	familyIPv4         = 0x01
	familyIPv6         = 0x02
)

// Detect asks the servers in order and returns the first public IPv4 address
// one of them reports, with the server that answered. servers accepts
// "host:port", "stun:host:port" and "stun:host" (port 3478). Each server gets
// at most perServer; ctx bounds the whole probe.
func Detect(ctx context.Context, servers []string, perServer time.Duration) (netip.Addr, string, error) {
	return detect(ctx, servers, perServer, false)
}

// Detect6 is Detect over IPv6: it returns the first global IPv6 address a
// server reports. Only the setup checks use it, to tell a connection without a
// public IPv4 address (IPv6 works, IPv4 does not) from one with a broken STUN.
func Detect6(ctx context.Context, servers []string, perServer time.Duration) (netip.Addr, string, error) {
	return detect(ctx, servers, perServer, true)
}

func detect(ctx context.Context, servers []string, perServer time.Duration, v6 bool) (netip.Addr, string, error) {
	if perServer <= 0 {
		perServer = DefaultTimeout
	}
	var errs []error
	for _, s := range servers {
		if ctx.Err() != nil {
			break
		}
		addr, err := query(ctx, s, perServer, v6)
		if err == nil {
			return addr, s, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", s, err))
	}
	if len(errs) == 0 {
		return netip.Addr{}, "", errors.New("no STUN server configured")
	}
	return netip.Addr{}, "", errors.Join(errs...)
}

// hostPort normalizes a STUN server entry to host:port.
func hostPort(entry string) string {
	e := strings.TrimSpace(entry)
	for _, p := range []string{"stuns:", "stun:"} {
		e = strings.TrimPrefix(e, p)
	}
	if _, _, err := net.SplitHostPort(e); err != nil {
		return net.JoinHostPort(strings.Trim(e, "[]"), "3478")
	}
	return e
}

func query(ctx context.Context, server string, timeout time.Duration, v6 bool) (netip.Addr, error) {
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var d net.Dialer
	network := "udp4"
	if v6 {
		network = "udp6"
	}
	conn, err := d.DialContext(qctx, network, hostPort(server))
	if err != nil {
		return netip.Addr{}, err
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := qctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	req, txid := bindingRequest()
	buf := make([]byte, 1500)
	// UDP drops packets: send up to three times within the deadline.
	for try := 0; try < 3; try++ {
		if _, err := conn.Write(req); err != nil {
			return netip.Addr{}, err
		}
		_ = conn.SetReadDeadline(time.Now().Add(timeout / 3))
		n, err := conn.Read(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() && qctx.Err() == nil {
				continue
			}
			return netip.Addr{}, err
		}
		addr, err := parseResponse(buf[:n], txid, v6)
		if err != nil {
			return netip.Addr{}, err
		}
		return addr, nil
	}
	return netip.Addr{}, errors.New("no answer")
}

func bindingRequest() (req []byte, txid [12]byte) {
	_, _ = rand.Read(txid[:])
	req = make([]byte, headerLen)
	binary.BigEndian.PutUint16(req[0:], typeBindingRequest)
	binary.BigEndian.PutUint32(req[4:], magicCookie)
	copy(req[8:], txid[:])
	return req, txid
}

// parseResponse extracts the reported address (IPv4, or IPv6 with v6) from a
// binding success response for the given transaction.
func parseResponse(b []byte, txid [12]byte, v6 bool) (netip.Addr, error) {
	if len(b) < headerLen || binary.BigEndian.Uint16(b) != typeBindingSuccess ||
		binary.BigEndian.Uint32(b[4:]) != magicCookie || string(b[8:20]) != string(txid[:]) {
		return netip.Addr{}, errors.New("not a binding success response")
	}
	end := headerLen + int(binary.BigEndian.Uint16(b[2:]))
	if end > len(b) {
		return netip.Addr{}, errors.New("truncated response")
	}
	var mapped netip.Addr
	for p := headerLen; p+4 <= end; {
		typ, l := binary.BigEndian.Uint16(b[p:]), int(binary.BigEndian.Uint16(b[p+2:]))
		if p+4+l > end {
			break
		}
		val := b[p+4 : p+4+l]
		switch typ {
		case attrXORMapped:
			if a, ok := decodeAddress(val, true, txid, v6); ok {
				return checkPublic(a)
			}
		case attrMappedAddress:
			if a, ok := decodeAddress(val, false, txid, v6); ok {
				mapped = a
			}
		}
		p += 4 + (l+3)&^3
	}
	if mapped.IsValid() {
		return checkPublic(mapped)
	}
	return netip.Addr{}, errors.New("no address of the asked family in response")
}

func decodeAddress(v []byte, xor bool, txid [12]byte, v6 bool) (netip.Addr, bool) {
	if len(v) < 8 {
		return netip.Addr{}, false
	}
	if v6 {
		return decodeIPv6(v, xor, txid)
	}
	if v[1] != familyIPv4 {
		return netip.Addr{}, false
	}
	var ip [4]byte
	copy(ip[:], v[4:8])
	if xor {
		var m [4]byte
		binary.BigEndian.PutUint32(m[:], magicCookie)
		for i := range ip {
			ip[i] ^= m[i]
		}
	}
	return netip.AddrFrom4(ip), true
}

// decodeIPv6 reads an IPv6 (XOR-)MAPPED-ADDRESS; the XOR key is the magic
// cookie followed by the transaction id.
func decodeIPv6(v []byte, xor bool, txid [12]byte) (netip.Addr, bool) {
	if len(v) < 20 || v[1] != familyIPv6 {
		return netip.Addr{}, false
	}
	var ip [16]byte
	copy(ip[:], v[4:20])
	if xor {
		var key [16]byte
		binary.BigEndian.PutUint32(key[:4], magicCookie)
		copy(key[4:], txid[:])
		for i := range ip {
			ip[i] ^= key[i]
		}
	}
	return netip.AddrFrom16(ip), true
}

// checkPublic rejects addresses that cannot be reached from the internet: a
// STUN server on the same network would report the private address.
func checkPublic(a netip.Addr) (netip.Addr, error) {
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return netip.Addr{}, fmt.Errorf("reported address %s is not public", a)
	}
	return a, nil
}
