package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// licenseOf names the SPDX license of a module from its license file.
func licenseOf(path string) (string, error) {
	dir, err := goOut("list", "-m", "-f", "{{.Dir}}", path)
	if err != nil {
		return "", fmt.Errorf("find %s: %w", path, err)
	}
	dir = strings.TrimSpace(dir)
	for _, name := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING", "LICENCE"} {
		text, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if id := classify(string(text)); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("%s: license not recognised; add its row to %s by hand", path, file)
}

func classify(text string) string {
	switch {
	case strings.Contains(text, "Apache License") && strings.Contains(text, "Version 2.0"):
		return "Apache-2.0"
	case strings.Contains(text, "Mozilla Public License"):
		return "MPL-2.0"
	case strings.Contains(text, "Redistribution and use in source and binary forms"):
		if strings.Contains(text, "Neither the name") {
			return "BSD-3-Clause"
		}
		return "BSD-2-Clause"
	case strings.Contains(text, "Permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(text, "Permission to use, copy, modify, and/or distribute"):
		return "ISC"
	}
	return ""
}
