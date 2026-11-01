package server

import (
	"net/http"
	"runtime"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/metrics"
)

// OpsStatus is the response of GET /api/admin/system/ops: the live operator
// view of the System tab, built from the same instruments as /api/metrics.
type OpsStatus struct {
	// IntervalSeconds is the spacing of History.
	IntervalSeconds int `json:"interval_seconds"`
	// History holds up to the last hour of samples, oldest first.
	History []OpsSample `json:"history"`
	Totals  OpsTotals   `json:"totals"`
	Pool    OpsPool     `json:"db_pool"`
	Process OpsProcess  `json:"process"`
}

// OpsTotals are counters since the process started.
type OpsTotals struct {
	Requests         uint64 `json:"requests"`
	Errors4xx        uint64 `json:"errors_4xx"`
	Errors5xx        uint64 `json:"errors_5xx"`
	RateLimited      uint64 `json:"rate_limited"`
	WSConnections    uint64 `json:"ws_connections"`
	WSDropped        uint64 `json:"ws_dropped"`
	MLSRelayed       uint64 `json:"mls_handshakes_relayed"`
	MLSRejected      uint64 `json:"mls_handshakes_rejected"`
	ICEConnected     uint64 `json:"ice_connected"`
	ICEFailed        uint64 `json:"ice_failed"`
	ICEDisconnected  uint64 `json:"ice_disconnected"`
	NackRequests     uint64 `json:"nack_requests"`
	KeyframeRequests uint64 `json:"keyframe_requests"`
	LayerSwitches    uint64 `json:"layer_switches"`
	EgressBytes      uint64 `json:"egress_bytes"`
	IngressBytes     uint64 `json:"ingress_bytes"`
	StorageErrors    uint64 `json:"storage_errors"`
	StorageOps       uint64 `json:"storage_operations"`
}

// OpsPool is the database connection pool.
type OpsPool struct {
	Total    int32 `json:"total"`
	Idle     int32 `json:"idle"`
	Acquired int32 `json:"acquired"`
	Max      int32 `json:"max"`
	// Waited counts acquisitions that had to wait for a free connection.
	Waited int64 `json:"waited"`
}

// OpsProcess are the process resources right now. RSSBytes is 0 where the
// platform does not report it.
type OpsProcess struct {
	CPUMillis     uint64 `json:"cpu_ms"`
	RSSBytes      uint64 `json:"rss_bytes"`
	HeapBytes     uint64 `json:"heap_bytes"`
	Goroutines    int    `json:"goroutines"`
	GCCycles      uint32 `json:"gc_cycles"`
	GCPauseMillis uint64 `json:"gc_pause_ms"`
}

// opsStatus handles GET /api/admin/system/ops.
//
// @Summary Operator metrics
// @Description Live counters and a short history (last hour, 10 s resolution) for the admin System tab. Contains no secrets and no user or channel ids. Owner only (403 otherwise).
// @ID getSystemOps
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} OpsStatus "Operator metrics."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/system/ops [get]
func (h *systemHandler) opsStatus(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, h.opsSnapshot())
	return nil
}

func (h *systemHandler) opsSnapshot() OpsStatus {
	history := []OpsSample{}
	var src *opsSource
	if h.ops != nil {
		history = append(history, h.ops.ring.Snapshot()...)
		src = h.ops.src
	}
	rd := src.read()
	mem := rd.mem
	st := OpsStatus{
		IntervalSeconds: int(opsInterval / time.Second),
		History:         history,
		Totals: OpsTotals{
			Requests:         metrics.HTTPRequests.Total(),
			Errors4xx:        metrics.HTTPRequests.Sum(1, "4xx"),
			Errors5xx:        metrics.HTTPRequests.Sum(1, "5xx"),
			RateLimited:      metrics.RateLimitHits.Total(),
			WSConnections:    metrics.WSConnections.Load(),
			WSDropped:        metrics.WSDropped.Total(),
			MLSRelayed:       metrics.MLSHandshakes.Sum(0, "relayed"),
			MLSRejected:      metrics.MLSHandshakes.Sum(0, "rejected"),
			ICEConnected:     metrics.SFUICE.Sum(0, "connected"),
			ICEFailed:        metrics.SFUICE.Sum(0, "failed"),
			ICEDisconnected:  metrics.SFUICE.Sum(0, "disconnected"),
			NackRequests:     metrics.SFUNackRequests.Load(),
			KeyframeRequests: metrics.SFUKeyframeRequests.Load(),
			LayerSwitches:    metrics.SFULayerSwitches.Load() + metrics.SFUCameraLayerSwitches.Load(),
			EgressBytes:      metrics.SFUEgressBytes.Load(),
			IngressBytes:     metrics.SFUIngressBytes.Load(),
			StorageErrors:    metrics.S3Operations.Sum(1, "error"),
			StorageOps:       metrics.S3Operations.Total(),
		},
		Pool: OpsPool{Total: rd.pool.Total, Idle: rd.pool.Idle, Acquired: rd.pool.Acquired, Max: rd.pool.Max, Waited: rd.pool.EmptyAcquireCount},
		Process: OpsProcess{
			CPUMillis: uint64(rd.proc.CPUSeconds * 1000), RSSBytes: rd.proc.RSSBytes, HeapBytes: mem.HeapAlloc,
			Goroutines: runtime.NumGoroutine(), GCCycles: mem.NumGC, GCPauseMillis: mem.PauseTotalNs / 1e6,
		},
	}
	return st
}
