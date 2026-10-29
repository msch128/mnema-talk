package setupcheck

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

// DefaultNTPServers are asked for the time when CLOCK_CHECK_ENABLED is on.
var DefaultNTPServers = []string{"time.cloudflare.com:123", "pool.ntp.org:123"}

// ntpEpochOffset is the number of seconds between the NTP epoch (1900) and
// the Unix epoch (1970).
const ntpEpochOffset = 2208988800

// MeasureClockSkew asks the NTP servers in order (SNTP, RFC 4330) and returns
// how far the local clock is ahead of the first answer (negative: behind).
// The half round trip is taken into account.
func MeasureClockSkew(ctx context.Context, servers []string, perServer time.Duration) (time.Duration, error) {
	var errs []error
	for _, s := range servers {
		if ctx.Err() != nil {
			break
		}
		skew, err := sntpQuery(ctx, s, perServer)
		if err == nil {
			return skew, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", s, err))
	}
	if len(errs) == 0 {
		return 0, errors.New("no NTP server configured")
	}
	return 0, errors.Join(errs...)
}

func sntpQuery(ctx context.Context, server string, timeout time.Duration) (time.Duration, error) {
	qctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(qctx, "udp", server)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := qctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	req := make([]byte, 48)
	req[0] = 0x1B // leap indicator 0, version 3, client mode
	sent := time.Now()
	if _, err := conn.Write(req); err != nil {
		return 0, err
	}
	resp := make([]byte, 128)
	n, err := conn.Read(resp)
	if err != nil {
		return 0, err
	}
	received := time.Now()
	if n < 48 || resp[0]&0x07 != 4 || resp[1] == 0 {
		return 0, errors.New("not a usable NTP response")
	}
	secs := binary.BigEndian.Uint32(resp[40:])
	frac := binary.BigEndian.Uint32(resp[44:])
	if secs == 0 {
		return 0, errors.New("server has no time")
	}
	server32 := time.Unix(int64(secs)-ntpEpochOffset, int64(frac)*1_000_000_000>>32)
	local := sent.Add(received.Sub(sent) / 2)
	return local.Sub(server32), nil
}
