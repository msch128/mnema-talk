// Command storm measures a reconnect storm: N users hold a WebSocket each,
// the server restarts, and every session comes back the way the web client
// does (stores/socket.ts and stores/chat/socket.ts): it waits out the
// client's backoff, asks GET /api/auth/me, opens the socket and, once it is
// open, reads the state again with the client's burst of REST requests. The
// run ends when every session is back and synced, and reports how long that
// took, the latency of the upgrades and of the first sync, and every refused
// or failed request.
//
// Configuration is read from the environment:
//
//	BASE_URL     instance URL (http:// or https://)
//	TOKENS       JSON file [{"id": "...", "token": "..."}]
//	CHANNEL_ID   text channel every user may view (the open channel's history is re-read)
//	SESSIONS     sessions, the first N tokens (default 1000)
//	BACKOFF      the window doubles from 1 s up to 30 s; "half" waits a random
//	             point in its second half (the web client), "fixed" the whole
//	             window (the client before), "full" anywhere in it; default half
//	CONNECT_PER_S   first connects per second (default 100)
//	READY_FILE   written once every session is connected and synced
//	TIMEOUT_S    seconds after the first drop the run gives up (default 300)
//	OUT          path of the JSON result
//
// Every session keeps its own connections, at most six as a browser does over
// HTTP/1.1, and sends its own X-Forwarded-For, so the run must reach the
// server from an address it trusts as a proxy (loopback by default): the
// per-address upgrade limit stays what it is.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprintln(os.Stderr, "storm:", err)
		os.Exit(1)
	}
}

func mainErr() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := newRun(cfg)
	res, err := r.execute(ctx)
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(data))
	if cfg.out != "" {
		return os.WriteFile(cfg.out, data, 0o600)
	}
	return nil
}
