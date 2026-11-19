package main

import (
	"strings"
	"testing"
)

func TestRefreshUpdatesVersionsAndDropsModulesNoBinaryCompiles(t *testing.T) {
	lines := []string{
		"## Server binary",
		"| `example.com/a/b` | v1.0.0 | MIT |",
		"| `example.com/gone/x` | v1.0.0 | MIT |",
		"| Module | Version | License |",
	}
	listed := map[string]bool{}
	got := strings.Join(refresh(lines, map[string]string{"example.com/a/b": "v1.2.0"}, listed), "\n")
	if !strings.Contains(got, "| `example.com/a/b` | v1.2.0 | MIT |") || strings.Contains(got, "gone") || !listed["example.com/a/b"] {
		t.Fatalf("refresh = %q", got)
	}
}

func TestInsertKeepsTheTableSortedAndInTheRightSection(t *testing.T) {
	lines := []string{
		"## Server binary",
		"| `example.com/a` | v1.0.0 | MIT |",
		"| `example.com/c` | v1.0.0 | MIT |",
		"## Updater binary",
		"| `example.com/z` | v1.0.0 | MIT |",
	}
	got := insert(lines, "## Server binary", "| `example.com/b` | v1.0.0 | MIT |")
	want := strings.Join([]string{
		"## Server binary",
		"| `example.com/a` | v1.0.0 | MIT |",
		"| `example.com/b` | v1.0.0 | MIT |",
		"| `example.com/c` | v1.0.0 | MIT |",
		"## Updater binary",
		"| `example.com/z` | v1.0.0 | MIT |",
	}, "\n")
	if strings.Join(got, "\n") != want {
		t.Fatalf("insert = %q", got)
	}
}

func TestClassifyNamesTheCommonLicenses(t *testing.T) {
	for text, want := range map[string]string{
		"Apache License\nVersion 2.0, January 2004":                                 "Apache-2.0",
		"Permission is hereby granted, free of charge, to any person":               "MIT",
		"Redistribution and use in source and binary forms ... Neither the name of": "BSD-3-Clause",
		"Redistribution and use in source and binary forms, with or without":        "BSD-2-Clause",
		"something custom": "",
	} {
		if got := classify(text); got != want {
			t.Errorf("classify(%q) = %q, want %q", text, got, want)
		}
	}
}
