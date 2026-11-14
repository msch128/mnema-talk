package bot

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

func TestDocsPermissionBitsAndExamples(t *testing.T) {
	botsDocBytes, err := os.ReadFile("../../doc/bots.md")
	if err != nil {
		t.Fatalf("read doc/bots.md: %v", err)
	}
	botsDoc := string(botsDocBytes)

	gwDocBytes, err := os.ReadFile("../../doc/gateway-events.md")
	if err != nil {
		t.Fatalf("read doc/gateway-events.md: %v", err)
	}
	gwDoc := string(gwDocBytes)

	bpDocBytes, err := os.ReadFile("../../doc/bot-platform.md")
	if err != nil {
		t.Fatalf("read doc/bot-platform.md: %v", err)
	}
	bpDoc := string(bpDocBytes)

	// 1. Verify warning against reusing Discord's PermissionFlagsBits in all three docs.
	for docName, content := range map[string]string{
		"doc/bots.md":           botsDoc,
		"doc/gateway-events.md": gwDoc,
		"doc/bot-platform.md":   bpDoc,
	} {
		if !strings.Contains(content, "PermissionFlagsBits") {
			t.Errorf("%s missing prominent notice about PermissionFlagsBits", docName)
		}
		if strings.Contains(content, `"3072"`) {
			t.Errorf("%s still contains legacy Discord bitfield example \"3072\"", docName)
		}
	}

	// 2. Verify table of permission keys -> bit values against perm.Catalog.
	catalogMap := make(map[string]perm.Info, len(perm.Catalog))
	for _, info := range perm.Catalog {
		catalogMap[info.Key] = info
	}

	tableRowRE := regexp.MustCompile(`\|\s*` + "`([a-z0-9_]+)`" + `\s*\|\s*` + "`([0-9]+)`" + `\s*\|\s*([^|]+)\s*\|`)
	matches := tableRowRE.FindAllStringSubmatch(botsDoc, -1)
	if len(matches) == 0 {
		t.Fatal("found no permission table rows in doc/bots.md")
	}

	checkedKeys := make(map[string]bool)
	for _, m := range matches {
		key, bitStr, name := m[1], m[2], strings.TrimSpace(m[3])
		info, ok := catalogMap[key]
		if !ok {
			t.Errorf("table contains unknown permission key: %q", key)
			continue
		}
		checkedKeys[key] = true
		wantBit := strconv.FormatUint(uint64(info.Bit), 10)
		if bitStr != wantBit {
			t.Errorf("permission %s bit = %s, want %s (catalog %d)", key, bitStr, wantBit, info.Bit)
		}
		if name != info.Name {
			t.Errorf("permission %s name = %q, want %q", key, name, info.Name)
		}
	}

	for _, info := range perm.Catalog {
		if !checkedKeys[info.Key] {
			t.Errorf("permission %s missing from doc/bots.md table", info.Key)
		}
	}

	// 3. Verify extracted quoted default_member_permissions / permissions examples.
	quotedPermRE := regexp.MustCompile(`"(?:default_member_permissions|permissions)":\s*"([0-9]+)"`)
	for docName, content := range map[string]string{
		"doc/bots.md":           botsDoc,
		"doc/gateway-events.md": gwDoc,
	} {
		sub := quotedPermRE.FindAllStringSubmatch(content, -1)
		if len(sub) == 0 {
			t.Errorf("%s has no quoted permission examples", docName)
		}
		for _, sm := range sub {
			valStr := sm[1]
			bits, err := strconv.ParseUint(valStr, 10, 64)
			if err != nil {
				t.Errorf("%s: example %q not a valid uint64: %v", docName, valStr, err)
				continue
			}
			p := perm.Set(bits)
			if !p.Valid() {
				t.Errorf("%s: example %q (bits %d) is not perm.Set.Valid()", docName, valStr, bits)
			}
		}
	}

	// 4. Verify prose examples match real constants.
	wantExamples := map[string]perm.Set{
		"6":      perm.ViewChannel | perm.SendMessages,
		"1":      perm.Administrator,
		"524288": perm.BanMembers,
	}
	for valStr, wantSet := range wantExamples {
		expectedStr := strconv.FormatUint(uint64(wantSet), 10)
		if valStr != expectedStr {
			t.Fatalf("example value %s does not match computed %s", valStr, expectedStr)
		}
		if !strings.Contains(botsDoc, `"`+valStr+`"`) {
			t.Errorf("doc/bots.md missing expected example %q", valStr)
		}
	}
}
