package server

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/ws"
)

// metricsHandler serves Prometheus-compatible text metrics.
func metricsHandler(p *db.Pool, hub *ws.Hub, voiceSFU *sfu.SFU) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder

		// Service status
		b.WriteString("# HELP mnema_up Whether the Mnema Talk instance is running\n")
		b.WriteString("# TYPE mnema_up gauge\n")
		b.WriteString("mnema_up 1\n\n")

		// Go runtime stats
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		b.WriteString("# HELP go_goroutines Number of goroutines currently existing\n")
		b.WriteString("# TYPE go_goroutines gauge\n")
		fmt.Fprintf(&b, "go_goroutines %d\n\n", runtime.NumGoroutine())

		b.WriteString("# HELP go_memstats_alloc_bytes Number of bytes allocated and still in use\n")
		b.WriteString("# TYPE go_memstats_alloc_bytes gauge\n")
		fmt.Fprintf(&b, "go_memstats_alloc_bytes %d\n\n", mem.Alloc)

		b.WriteString("# HELP go_memstats_sys_bytes Number of bytes obtained from system\n")
		b.WriteString("# TYPE go_memstats_sys_bytes gauge\n")
		fmt.Fprintf(&b, "go_memstats_sys_bytes %d\n\n", mem.Sys)

		b.WriteString("# HELP go_memstats_heap_objects Number of allocated objects\n")
		b.WriteString("# TYPE go_memstats_heap_objects gauge\n")
		fmt.Fprintf(&b, "go_memstats_heap_objects %d\n\n", mem.HeapObjects)

		// DB pool stats
		if p != nil {
			stat := p.Stat()
			b.WriteString("# HELP mnema_db_connections_total Total connections in pool\n")
			b.WriteString("# TYPE mnema_db_connections_total gauge\n")
			fmt.Fprintf(&b, "mnema_db_connections_total %d\n\n", stat.TotalConns())

			b.WriteString("# HELP mnema_db_connections_acquired Connections currently acquired by workers\n")
			b.WriteString("# TYPE mnema_db_connections_acquired gauge\n")
			fmt.Fprintf(&b, "mnema_db_connections_acquired %d\n\n", stat.AcquiredConns())

			b.WriteString("# HELP mnema_db_connections_idle Idle connections in pool\n")
			b.WriteString("# TYPE mnema_db_connections_idle gauge\n")
			fmt.Fprintf(&b, "mnema_db_connections_idle %d\n\n", stat.IdleConns())

			b.WriteString("# HELP mnema_db_connections_max Maximum connections configured for pool\n")
			b.WriteString("# TYPE mnema_db_connections_max gauge\n")
			fmt.Fprintf(&b, "mnema_db_connections_max %d\n\n", stat.MaxConns())
		}

		// WebSocket & Voice stats
		if hub != nil {
			b.WriteString("# HELP mnema_ws_online_users Number of distinct online users\n")
			b.WriteString("# TYPE mnema_ws_online_users gauge\n")
			fmt.Fprintf(&b, "mnema_ws_online_users %d\n\n", hub.OnlineCount())
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(b.String()))
	}
}
