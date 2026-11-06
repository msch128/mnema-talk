package server

import (
	"net/http"
	"runtime"

	"github.com/msch128/mnema-talk/internal/capacity"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// capacityWindow is how many chart samples (10 s each) the figures average
// over: one minute.
const capacityWindow = 6

// cgroupRoot is where the container's limits are read from.
const cgroupRoot = "/sys/fs/cgroup"

// CapacityStatus is the response of GET /api/admin/status/capacity.
type CapacityStatus struct {
	// CPUCores is the CPU the server may use; CPUUsedCores what it used over
	// the last minute.
	CPUCores     float64 `json:"cpu_cores"`
	CPUUsedCores float64 `json:"cpu_used_cores"`
	// MemoryLimitBytes is the container limit (APP_MEM_LIMIT); 0 when unknown.
	MemoryLimitBytes uint64 `json:"memory_limit_bytes"`
	RSSBytes         uint64 `json:"rss_bytes"`
	HeapBytes        uint64 `json:"heap_bytes"`
	// EgressMbps is the media the server sends, averaged over the last minute.
	EgressMbps float64 `json:"egress_mbps"`
	EgressPPS  uint64  `json:"egress_pps"`
	IngressPPS uint64  `json:"ingress_pps"`
	// Peers are media connections now; ScreenShares live screen shares.
	Peers        int `json:"peers"`
	ScreenShares int `json:"screen_shares"`
	// Collecting is true in the first seconds after the start, before the
	// sampler has taken a measurement: the figures are not real yet.
	Collecting bool `json:"collecting"`
	// Estimate is the planning estimate (see doc/benchmark.md).
	Estimate CapacityEstimate `json:"estimate"`
}

// CapacityEstimate says how many more members the server can take.
type CapacityEstimate struct {
	// MoreMembers: each one watching a 1.5 Mbit/s screen share, planning at
	// half of the CPU. 0 with Status memory or busy.
	MoreMembers int `json:"more_members"`
	// Basis is "observed" when this host's own cost per packet was used,
	// "table" when the benchmark values were (too little traffic).
	Basis string `json:"basis" enums:"table,observed"`
	// Scale is this host's observed CPU cost divided by the model's.
	Scale float64 `json:"scale"`
	// PerMemberMillicores is the CPU one more member costs.
	PerMemberMillicores float64 `json:"per_member_millicores"`
	// PlanCores is the CPU the estimate allows itself to use.
	PlanCores float64 `json:"plan_cores"`
	// Status is ok, busy (past the plan already) or memory (RSS near the limit).
	Status string `json:"status" enums:"ok,busy,memory"`
	// UploadLimitMbps is VOICE_UPLOAD_LIMIT_MBIT, the upload the scenarios
	// may use; 0 when it is not set and upload is only reported, not limited.
	UploadLimitMbps int `json:"upload_limit_mbps" binding:"optional"`
	// Scenarios is how many people one Talk of each kind can hold on what is
	// left of the plan; empty unless Status is ok.
	Scenarios []CapacityScenario `json:"scenarios" binding:"optional"`
}

// CapacityScenario is one row of the estimate: the largest Talk of one kind.
type CapacityScenario struct {
	// ID: chat (text only), broadcast (3 speakers, the rest listen),
	// conversation (everyone talks), screen (one share and 3 speakers, the
	// rest watch), video (everyone talks with the camera on).
	ID string `json:"id" enums:"chat,broadcast,conversation,screen,video"`
	// Members is how many people fit.
	Members int `json:"members"`
	// Limit is the resource the next person would exceed.
	Limit string `json:"limit" enums:"cpu,memory,upload"`
	// CPUCores, UploadMbps and MemoryBytes are the cost at Members.
	CPUCores    float64 `json:"cpu_cores"`
	UploadMbps  float64 `json:"upload_mbps"`
	MemoryBytes uint64  `json:"memory_bytes"`
	// Basis is measured when the shape was run in doc/benchmark.md, derived
	// when it is built from the measured costs.
	Basis string `json:"basis" enums:"measured,derived"`
	// MeasuredUpTo is the largest clean run of the shape (0 for derived).
	MeasuredUpTo int `json:"measured_up_to"`
	// Extrapolated is true when Members is beyond MeasuredUpTo.
	Extrapolated bool `json:"extrapolated"`
}

// capacityStatus handles GET /api/admin/status/capacity.
//
// @Summary Server capacity
// @Description Live CPU, memory and media rates of the server over the last minute, an estimate of how many more members it can take, each watching a screen share, and per usage scenario how many people one Talk can hold, with the resource that runs out first. The estimate uses the cost model of doc/benchmark.md, scaled by this host's observed CPU per packet when there is enough traffic; it is an estimate for planning, not a promise. Owner or Administrator only (403 otherwise).
// @ID getAdminCapacity
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} CapacityStatus "Capacity figures and estimate."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may do this."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/status/capacity [get]
func (h *systemHandler) capacityStatus(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, h.capacitySnapshot())
	return nil
}

func (h *systemHandler) capacitySnapshot() CapacityStatus {
	var samples []OpsSample
	if h.ops != nil {
		samples = h.ops.ring.Snapshot()
	}
	st := averageSamples(samples[max(0, len(samples)-capacityWindow):])
	st.CPUCores = capacity.Cores(cgroupRoot)
	st.MemoryLimitBytes = capacity.MemoryLimit(cgroupRoot)
	upload := 0
	if h.cfg != nil {
		upload = h.cfg.VoiceUploadLimitMbit
	}
	st.Estimate = capacityEstimate(st, upload)
	return st
}

// averageSamples averages the rates over the window and takes the memory and
// call figures from its newest sample; with no sample it falls back to the
// process as it is now.
func averageSamples(s []OpsSample) CapacityStatus {
	var st CapacityStatus
	if len(s) == 0 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		st.HeapBytes = ms.HeapAlloc
		st.Collecting = true
		return st
	}
	var cpu, bps, epps, ipps float64
	for _, x := range s {
		cpu += float64(x.CPUMillicores) / 1000
		bps += float64(x.EgressBps)
		epps += float64(x.EgressPPS)
		ipps += float64(x.IngressPPS)
	}
	n := float64(len(s))
	last := s[len(s)-1]
	st.CPUUsedCores, st.EgressMbps = cpu/n, bps/n/1e6
	st.EgressPPS, st.IngressPPS = uint64(epps/n), uint64(ipps/n)
	st.RSSBytes, st.HeapBytes = last.RSSBytes, last.HeapBytes
	st.Peers, st.ScreenShares = last.Peers, last.ScreenShares
	return st
}

func capacityEstimate(st CapacityStatus, uploadLimitMbps int) CapacityEstimate {
	r := capacity.Estimate(capacity.Input{
		Cores: st.CPUCores, UsedCores: st.CPUUsedCores,
		EgressPPS: float64(st.EgressPPS), IngressPPS: float64(st.IngressPPS),
		Peers: st.Peers, RSSBytes: st.RSSBytes, MemoryLimit: st.MemoryLimitBytes,
		UploadLimitMbps: float64(uploadLimitMbps),
	})
	e := CapacityEstimate{
		MoreMembers: r.MoreMembers, Basis: r.Basis, Scale: r.Scale,
		PerMemberMillicores: r.PerMemberCores * 1000, PlanCores: r.PlanCores, Status: r.Status,
		UploadLimitMbps: uploadLimitMbps, Scenarios: make([]CapacityScenario, 0, len(r.Scenarios)),
	}
	for _, s := range r.Scenarios {
		e.Scenarios = append(e.Scenarios, CapacityScenario{
			ID: s.Scenario.ID, Members: s.Fit.Members, Limit: s.Fit.Limit,
			CPUCores: s.Fit.Load.Cores, UploadMbps: s.Fit.Load.UploadMbps, MemoryBytes: uint64(s.Fit.Load.MemoryBytes),
			Basis: s.Scenario.Basis, MeasuredUpTo: s.Scenario.MeasuredUpTo, Extrapolated: s.Fit.Extrapolated,
		})
	}
	return e
}
