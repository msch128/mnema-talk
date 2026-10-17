package httpx

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A client that stalls mid-body is cut off after JSONReadTimeout, not after the
// server's long ReadTimeout. The real timeout is shortened for the test.
func TestMaxBodyShortensTheReadDeadlineOfSmallBodies(t *testing.T) {
	old := jsonReadTimeout
	jsonReadTimeout = 300 * time.Millisecond
	defer func() { jsonReadTimeout = old }()

	readErr := make(chan error, 1)
	h := MaxBody(DefaultMaxBody)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readErr <- err
	}))
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadTimeout = 10 * time.Minute
	srv.Start()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\nab"))
	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("stalled body read succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled body was not cut off")
	}
}

func TestMaxBodyKeepsLongDeadlinesForUploads(t *testing.T) {
	old := jsonReadTimeout
	jsonReadTimeout = 200 * time.Millisecond
	defer func() { jsonReadTimeout = old }()

	readErr := make(chan error, 1)
	h := MaxBody(DefaultMaxBody * 10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readErr <- err
	}))
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadTimeout = 10 * time.Minute
	srv.Start()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 4\r\n\r\nab"))
	time.Sleep(600 * time.Millisecond) // past the short deadline: still waiting
	_, _ = conn.Write([]byte("cd"))
	select {
	case err := <-readErr:
		if err != nil {
			t.Fatalf("upload body cut off: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload body never finished")
	}
}
