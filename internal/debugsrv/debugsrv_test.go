package debugsrv

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestCheckAddrAcceptsOnlyLoopback(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:6060", "localhost:6060", "[::1]:6060", "127.0.0.2:1"} {
		if err := CheckAddr(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "6060", ":6060", "0.0.0.0:6060", "[::]:6060", "192.168.1.5:6060", "example.com:6060", "127.0.0.1", "127.0.0.1:"} {
		if err := CheckAddr(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestStartIsOffWithoutAnAddress(t *testing.T) {
	addr, err := Start(context.Background(), "")
	if addr != "" || err != nil {
		t.Fatalf("addr %q err %v", addr, err)
	}
}

func TestStartRefusesANonLoopbackBind(t *testing.T) {
	if _, err := Start(context.Background(), "0.0.0.0:0"); err == nil {
		t.Fatal("bound a non-loopback address")
	}
}

func TestStartReportsAnAddressInUse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr, err := Start(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Start(ctx, addr); err == nil {
		t.Fatal("second listener on the same port accepted")
	}
}

func TestServesProfilesUntilTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	addr, err := Start(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/cmdline", "/debug/pprof/heap", "/debug/pprof/symbol"} {
		resp, err := http.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", path, resp.StatusCode)
		}
	}
	resp, err := http.Get("http://" + addr + "/other")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/other: status %d", resp.StatusCode)
	}
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := http.Get("http://" + addr + "/debug/pprof/"); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("still serving after the context ended")
}
