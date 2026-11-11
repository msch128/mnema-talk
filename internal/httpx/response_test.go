package httpx

import (
	"testing"
	"time"
)

func TestRetryAfterRoundsUpOnce(t *testing.T) {
	for d, want := range map[time.Duration]string{2 * time.Second: "2", 2100 * time.Millisecond: "3", time.Millisecond: "1"} {
		if got := retryAfterSeconds(d); got != want {
			t.Fatalf("%v -> %s, want %s", d, got, want)
		}
	}
}
