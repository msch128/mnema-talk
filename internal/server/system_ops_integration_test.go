//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestSystemOpsIsAdminOnlyAndReportsLiveData(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")

	if res := a.anon().get("/api/admin/system/ops"); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d %s", res.status, res.body)
	}
	if res := max.get("/api/admin/system/ops"); res.status != http.StatusForbidden {
		t.Fatalf("member: %d %s", res.status, res.body)
	}
	res := admin.get("/api/admin/system/ops")
	if res.status != http.StatusOK || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("admin: %d %s", res.status, res.body)
	}
	var st OpsStatus
	res.decode(t, &st)
	if st.IntervalSeconds != 10 || st.History == nil {
		t.Fatalf("status=%+v", st)
	}
	if st.Totals.Requests == 0 || st.Pool.Max == 0 || st.Process.Goroutines == 0 {
		t.Fatalf("live data missing: %+v", st)
	}
	if strings.Contains(string(res.body), max.user.ID.String()) {
		t.Fatal("ops payload carries a user id")
	}
}
