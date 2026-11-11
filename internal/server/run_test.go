package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
)

func TestRunBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	if err := Run(context.Background(), &config.Config{BindAddr: "127.0.0.1", Port: port}, http.NotFoundHandler()); err == nil || !strings.Contains(err.Error(), "http server:") {
		t.Fatalf("bind failure: %v", err)
	}
}

func TestPreviewDenyRefreshRepeatsUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls, done := make(chan struct{}, 10), make(chan struct{})
	go func() {
		refreshPreviewDeny(ctx, time.Millisecond, func() {
			select {
			case calls <- struct{}{}:
			default:
			}
		})
		close(done)
	}()
	for range 2 {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatal("deny list was not refreshed")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh survived cancellation")
	}
}

func TestRunGracefulShutdownDrainsRequest(t *testing.T) {
	// Choose a loopback port, then let Run own its real HTTP listener.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	_ = listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, &config.Config{BindAddr: "127.0.0.1", Port: port}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-release
			_, _ = w.Write([]byte("drained"))
		}))
	}()
	result := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		deadline := time.Now().Add(3 * time.Second)
		for {
			resp, err := client.Get("http://" + address)
			if err != nil && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
				continue
			}
			if err != nil {
				result <- err
				return
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err == nil && string(body) != "drained" {
				err = errors.New("request was not drained")
			}
			result <- err
			return
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not serve")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before active request drained: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown hung")
	}
}
