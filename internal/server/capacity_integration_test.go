//go:build integration

package server

import (
	"net/http"
	"testing"
)

func TestCapacityIsAdminOnlyAndWorksWithoutTraffic(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	member := a.register(admin, "pat")

	if res := a.anon().get("/api/admin/status/capacity"); res.status != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", res.status)
	}
	if res := member.get("/api/admin/status/capacity"); res.status != http.StatusForbidden {
		t.Errorf("member: %d", res.status)
	}
	res := admin.get("/api/admin/status/capacity")
	if res.status != http.StatusOK || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("admin: %d %s", res.status, res.body)
	}
	var st CapacityStatus
	res.decode(t, &st)
	if st.CPUCores <= 0 || st.Estimate.Status != "ok" || st.Estimate.MoreMembers <= 0 || st.Estimate.Basis != "table" {
		t.Fatalf("idle server: %+v", st)
	}
}
