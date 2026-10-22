//go:build integration

package server

import (
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/sfu"
)

const alertRulesFile = "../../deploy/monitoring/mnema-alerts.yml"

// Series that Prometheus or the operator's own jobs produce, not the app.
var externalAlertMetrics = map[string]bool{
	"up":            true,
	"probe_success": true,
	"mnema_backup_last_success_timestamp_seconds": true,
}

var (
	promString   = regexp.MustCompile(`"[^"]*"`)
	promMatchers = regexp.MustCompile(`\{[^}]*\}`)
	promGrouping = regexp.MustCompile(`\b(by|without|on|ignoring|group_left|group_right)\s*\([^)]*\)`)
	promIdent    = regexp.MustCompile(`[a-zA-Z_:][a-zA-Z0-9_:]*(\s*\()?`)
	promKeywords = map[string]bool{"and": true, "or": true, "unless": true, "bool": true, "offset": true}
	promNumber   = regexp.MustCompile(`\b[0-9][0-9a-zA-Z.]*`)
	promSuffix   = regexp.MustCompile(`_(bucket|sum|count)$`)
	typeLine     = regexp.MustCompile(`(?m)^# TYPE (\S+) `)
)

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// alertExprs returns the expr fields of a rules file (single-line values and
// `|` blocks).
func alertExprs(rules string) []string {
	var exprs []string
	lines := strings.Split(rules, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "expr:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "expr:"))
		if value != "|" {
			exprs = append(exprs, value)
			continue
		}
		var block []string
		for i+1 < len(lines) && (strings.TrimSpace(lines[i+1]) == "" || indentOf(lines[i+1]) > indentOf(lines[i])) {
			i++
			block = append(block, lines[i])
		}
		exprs = append(exprs, strings.Join(block, "\n"))
	}
	return exprs
}

// exprMetricNames returns the metric names an expression selects.
func exprMetricNames(expr string) []string {
	var kept []string
	for _, l := range strings.Split(expr, "\n") {
		before, _, _ := strings.Cut(l, "#")
		kept = append(kept, before)
	}
	e := strings.Join(kept, " ")
	e = promString.ReplaceAllString(e, "")
	e = promNumber.ReplaceAllString(e, "") // 800e6, 10m
	e = promGrouping.ReplaceAllString(e, "")
	e = promMatchers.ReplaceAllString(e, "")
	var names []string
	for _, tok := range promIdent.FindAllString(e, -1) {
		if strings.HasSuffix(tok, "(") || promKeywords[tok] {
			continue
		}
		names = append(names, strings.TrimSpace(tok))
	}
	return names
}

func TestAlertRulesNameOnlyExportedMetrics(t *testing.T) {
	raw, err := os.ReadFile(alertRulesFile)
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	for _, e := range alertExprs(string(raw)) {
		for _, name := range exprMetricNames(e) {
			used[name] = true
		}
	}
	if !used["mnema_storage_up"] || !used["mnema_sfu_ice_transitions_total"] || !used["mnema_http_request_duration_seconds_bucket"] {
		t.Fatalf("expression scan found %v, expected the known alert metrics", used)
	}

	voice, err := sfu.NewSFU(50000, 50100, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer voice.Close()
	a := newAppWithDeps(t, false, func(cfg *config.Config) { cfg.MetricsToken = "fixture-metrics-token" }, func(deps *Deps) { deps.SFU = voice })
	res := a.anon().do(http.MethodGet, "/api/metrics", nil, map[string]string{"Authorization": "Bearer fixture-metrics-token"})
	if res.status != http.StatusOK {
		t.Fatalf("metrics=%d %s", res.status, res.body)
	}
	exported := map[string]bool{}
	for _, m := range typeLine.FindAllStringSubmatch(string(res.body), -1) {
		exported[m[1]] = true
	}
	for name := range used {
		base := promSuffix.ReplaceAllString(name, "")
		external := externalAlertMetrics[name] || (name == "process_resident_memory_bytes" && runtime.GOOS != "linux")
		if !exported[name] && !exported[base] && !external {
			t.Errorf("alert rules use %q, which /api/metrics does not export", name)
		}
	}
}
