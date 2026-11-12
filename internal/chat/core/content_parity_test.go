package core

import (
	"encoding/json"
	"os"
	"testing"
	"unicode/utf8"
)

// One rule, two implementations (2026-11 defect hunt, lane 3, pair 5): what a
// message may hold. The composer trims and counts code points
// (web/src/chat/lib/composerLimits.ts), the server trims with Go's white space
// and counts runes (ValidateContent). testdata/content_vectors.json holds what
// the client sends; web/src/chat/lib/contentParity.test.ts compares.
//
//	CONTENT_PARITY_UPDATE=1 go test -run TestContentVectorsMatchTheServer ./internal/chat/core

type contentVector struct {
	Sent  string `json:"sent"`
	OK    bool   `json:"ok"`
	Runes int    `json:"runes"`
}

func TestContentVectorsMatchTheServer(t *testing.T) {
	const file = "testdata/content_vectors.json"
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var vs []contentVector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatal(err)
	}
	if len(vs) == 0 {
		t.Fatal("no vectors")
	}
	update := os.Getenv("CONTENT_PARITY_UPDATE") == "1"
	for i, v := range vs {
		stored, err := ValidateContent(v.Sent, false)
		got := contentVector{Sent: v.Sent, OK: err == nil, Runes: utf8.RuneCountInString(stored)}
		if update {
			vs[i] = got
		} else if got != v {
			t.Errorf("vector %d: server %+v, the file says %+v (rerun with CONTENT_PARITY_UPDATE=1)", i, got, v)
		}
	}
	if update {
		out, err := json.MarshalIndent(vs, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
