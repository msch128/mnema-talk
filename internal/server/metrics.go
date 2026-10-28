package server

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/version"
)

// metricsHandler serves Prometheus-compatible text metrics.
//
// @Summary Prometheus metrics
// @Description Only registered when METRICS_TOKEN is configured. Failures answer 401 with a plain-text body, not the JSON error shape.
// @ID getMetrics
// @Tags System
// @Produce plain
// @Security bearerAuth
// @Success 200 {string} string "Prometheus text exposition format."
// @Failure 401 {string} string "Missing or wrong bearer token (plain text 'unauthorized', WWW-Authenticate: Bearer)."
// @Header 401 {string} WWW-Authenticate "Bearer challenge for the metrics endpoint."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/metrics [get]
func metricsHandler(src *opsSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		rd := src.read() // once per scrape: it stops the world and probes storage
		writeProcessMetrics(&b, src, rd)
		writeDependencyMetrics(&b, rd)
		metrics.WriteInstruments(&b)

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(b.String())); err != nil {
			return
		}
	}
}

// writeProcessMetrics writes build, process and Go runtime metrics.
func writeProcessMetrics(b *strings.Builder, src *opsSource, rd opsReading) {
	ver := "dev"
	if src != nil && src.version != "" {
		ver = src.version
	}
	metrics.WriteGauge(b, "mnema_up", "Whether the Mnema Talk instance is running", 1)
	b.WriteString("# HELP mnema_build_info Build version and revision (value is always 1)\n# TYPE mnema_build_info gauge\n")
	fmt.Fprintf(b, "mnema_build_info{version=%q,revision=%q,go_version=%q} 1\n\n", ver, version.Commit(), version.GoVersion())
	metrics.WriteFloatGauge(b, "process_start_time_seconds", "Start time of the process since the unix epoch", float64(metrics.ProcessStart().UnixNano())/1e9)
	metrics.WriteFloatCounter(b, "process_cpu_seconds_total", "User and system CPU time spent by the process", rd.proc.CPUSeconds)
	if rd.proc.RSSBytes > 0 {
		metrics.WriteGauge(b, "process_resident_memory_bytes", "Resident memory of the process (Linux only)", int64(rd.proc.RSSBytes))
	}
	metrics.WriteGauge(b, "go_goroutines", "Number of goroutines currently existing", int64(runtime.NumGoroutine()))
	metrics.WriteGauge(b, "go_memstats_alloc_bytes", "Number of bytes allocated and still in use", int64(rd.mem.Alloc))
	metrics.WriteGauge(b, "go_memstats_sys_bytes", "Number of bytes obtained from system", int64(rd.mem.Sys))
	metrics.WriteGauge(b, "go_memstats_heap_inuse_bytes", "Heap bytes in use by spans", int64(rd.mem.HeapInuse))
	metrics.WriteGauge(b, "go_memstats_heap_objects", "Number of allocated objects", int64(rd.mem.HeapObjects))
	metrics.WriteFloatCounter(b, "go_gc_cycles_total", "Completed garbage collection cycles", float64(rd.mem.NumGC))
	metrics.WriteFloatCounter(b, "go_gc_pause_seconds_total", "Cumulative stop-the-world pause time of the garbage collector", float64(rd.mem.PauseTotalNs)/1e9)
}

// writeDependencyMetrics writes the database pool, realtime hub, SFU and
// storage gauges.
func writeDependencyMetrics(b *strings.Builder, rd opsReading) {
	if rd.hasPool {
		p := rd.pool
		metrics.WriteGauge(b, "mnema_db_connections_total", "Total connections in pool", int64(p.Total))
		metrics.WriteGauge(b, "mnema_db_connections_acquired", "Connections currently acquired by workers", int64(p.Acquired))
		metrics.WriteGauge(b, "mnema_db_connections_idle", "Idle connections in pool", int64(p.Idle))
		metrics.WriteGauge(b, "mnema_db_connections_max", "Maximum connections configured for pool", int64(p.Max))
		metrics.WriteFloatCounter(b, "mnema_db_acquires_total", "Connection acquisitions", float64(p.AcquireCount))
		metrics.WriteFloatCounter(b, "mnema_db_acquires_waited_total", "Acquisitions that had to wait for a free connection", float64(p.EmptyAcquireCount))
		metrics.WriteFloatCounter(b, "mnema_db_acquires_canceled_total", "Acquisitions abandoned by the caller", float64(p.CanceledAcquireCount))
		metrics.WriteFloatCounter(b, "mnema_db_acquire_seconds_total", "Cumulative time spent acquiring connections", p.AcquireSeconds)
	}
	metrics.WriteGauge(b, "mnema_ws_online_users", "Number of distinct online users", int64(rd.ws.OnlineUsers))
	metrics.WriteGauge(b, "mnema_ws_connections", "Open realtime connections", int64(rd.ws.Connections))
	metrics.WriteGauge(b, "mnema_voice_rooms", "Voice channels with at least one member", int64(rd.ws.VoiceRooms))
	metrics.WriteGauge(b, "mnema_voice_participants", "Members in voice channels", int64(rd.ws.VoiceParticipants))
	metrics.WriteGauge(b, "mnema_sfu_rooms", "Voice channels with at least one media connection", int64(rd.sfu.Rooms))
	metrics.WriteGauge(b, "mnema_sfu_peers", "Media connections", int64(rd.sfu.Peers))
	b.WriteString("# HELP mnema_sfu_tracks Published tracks by kind\n# TYPE mnema_sfu_tracks gauge\n")
	fmt.Fprintf(b, "mnema_sfu_tracks{kind=\"audio\"} %d\nmnema_sfu_tracks{kind=\"video\"} %d\n\n", rd.sfu.AudioTracks, rd.sfu.VideoTracks)
	metrics.WriteGauge(b, "mnema_sfu_video_rooms", "Voice channels in which at least one member shares a screen or camera", int64(rd.sfu.VideoRooms))
	metrics.WriteGauge(b, "mnema_sfu_screen_shares", "Screen shares currently forwarded by the SFU", int64(rd.sfu.ScreenShares))
	metrics.WriteGauge(b, "mnema_sfu_cameras", "Cameras currently forwarded by the SFU", int64(rd.sfu.Cameras))
	up := int64(0)
	if rd.storageU {
		up = 1
	}
	metrics.WriteGauge(b, "mnema_storage_up", "Whether object storage is reachable (0 also when none is configured)", up)
}

// requireBearer only lets requests through that carry "Authorization: Bearer <token>".
func requireBearer(token string, next http.Handler) http.HandlerFunc {
	want := []byte("Bearer " + token)
	return func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}
}
