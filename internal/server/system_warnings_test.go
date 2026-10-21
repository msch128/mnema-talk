package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/opwarn"
)

func querySystemStatusWarnings(t *testing.T, h *systemHandler) SystemStatus {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := h.status(rec, httptest.NewRequest(http.MethodGet, "/api/admin/system", nil)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"warnings":[`) {
		t.Fatalf("warnings must be an array, body: %s", rec.Body.String())
	}
	var st SystemStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func assertSystemWarnings(t *testing.T, st SystemStatus) {
	t.Helper()
	if len(st.Warnings) != 2 {
		t.Fatalf("want 2 warnings, got: %+v", st.Warnings)
	}
	if st.Warnings[0].Code != opwarn.TrustedProxyBroad {
		t.Errorf("warning 0 code = %v, want %v", st.Warnings[0].Code, opwarn.TrustedProxyBroad)
	}
	if st.Warnings[1].Code != opwarn.WebRTCUDPMuxDisabled {
		t.Errorf("warning 1 code = %v, want %v", st.Warnings[1].Code, opwarn.WebRTCUDPMuxDisabled)
	}
	if st.Warnings[1].Value != 51 {
		t.Errorf("warning 1 value = %v, want 51", st.Warnings[1].Value)
	}
	if st.Warnings[1].Count != 2 {
		t.Errorf("warning 1 count = %v, want 2", st.Warnings[1].Count)
	}
}

func TestSystemStatusReportsOperatorWarnings(t *testing.T) {
	reg := opwarn.New()
	h := &systemHandler{cfg: &config.Config{}, version: "0.8.0", warnings: reg}

	if st := querySystemStatusWarnings(t, h); len(st.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", st.Warnings)
	}
	reg.Raise(opwarn.WebRTCUDPMuxDisabled, 51)
	reg.Raise(opwarn.WebRTCUDPMuxDisabled, 51)
	reg.Raise(opwarn.TrustedProxyBroad, 1)
	assertSystemWarnings(t, querySystemStatusWarnings(t, h))
}
