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

func TestSystemStatusReportsOperatorWarnings(t *testing.T) {
	reg := opwarn.New()
	h := &systemHandler{cfg: &config.Config{}, version: "0.8.0", warnings: reg}

	get := func() SystemStatus {
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
	if st := get(); len(st.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", st.Warnings)
	}
	reg.Raise(opwarn.WebRTCUDPMuxDisabled, 51)
	reg.Raise(opwarn.WebRTCUDPMuxDisabled, 51)
	reg.Raise(opwarn.TrustedProxyBroad, 1)
	st := get()
	if len(st.Warnings) != 2 || st.Warnings[0].Code != opwarn.TrustedProxyBroad ||
		st.Warnings[1].Code != opwarn.WebRTCUDPMuxDisabled || st.Warnings[1].Value != 51 || st.Warnings[1].Count != 2 {
		t.Fatalf("warnings = %+v", st.Warnings)
	}
}
