//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/sfu"
)

func TestRouterMetricsExposeRealPoolHubAndVoice(t *testing.T) {
	voice, err := sfu.NewSFU(50000, 50100, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer voice.Close()
	a := newAppWithDeps(t, false, func(cfg *config.Config) { cfg.MetricsToken = "fixture-metrics-token" }, func(deps *Deps) { deps.SFU = voice })
	a.anon().get("/api/health")
	res := a.anon().do(http.MethodGet, "/api/metrics", nil, map[string]string{"Authorization": "Bearer fixture-metrics-token"})
	if res.status != http.StatusOK {
		t.Fatalf("metrics=%d %s", res.status, res.body)
	}
	for _, metric := range []string{"mnema_db_connections_total", "mnema_db_connections_acquired", "mnema_db_connections_idle", "mnema_db_connections_max", "mnema_ws_online_users 0", "mnema_sfu_video_rooms 0", "mnema_build_info{", "process_cpu_seconds_total", "go_gc_pause_seconds_total", `mnema_sfu_tracks{kind="audio"} 0`, "mnema_storage_up ", `mnema_http_requests_total{route="/api/health",class="2xx"}`, "mnema_http_request_duration_seconds_bucket", "mnema_db_acquires_total"} {
		if !strings.Contains(string(res.body), metric) {
			t.Fatalf("missing %s", metric)
		}
	}
	admin := a.seedAdmin()
	var status SystemStatus
	admin.get("/api/admin/system").decode(t, &status)
	if !status.Health.Voice.Enabled || !status.Health.Database.Reachable {
		t.Fatalf("health=%+v", status.Health)
	}
}

func TestSystemHealthDetectsPendingMigrations(t *testing.T) {
	a := newApp(t, false)
	ctx := context.Background()
	var name, checksum string
	if err := a.db.QueryRow(ctx, `SELECT filename, checksum FROM schema_migrations ORDER BY filename DESC LIMIT 1`).Scan(&name, &checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx, `DELETE FROM schema_migrations WHERE filename=$1`, name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := a.db.Exec(ctx, `INSERT INTO schema_migrations(filename, checksum) VALUES ($1,$2)`, name, checksum); err != nil {
			t.Error(err)
		}
	})
	h := &systemHandler{cfg: &config.Config{}, db: a.db}
	if health := h.health(ctx); !health.Database.Reachable || health.Database.PendingMigrations != 1 {
		t.Fatalf("database=%+v", health.Database)
	}
}
