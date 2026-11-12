package mention

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// One rule, two implementations: which @names a message mentions. Names
// decides who is notified; the web client draws the pills with its own
// pattern (web/src/shared/lib/markdown/mentions.ts). testdata/parity.json holds
// messages with the names read here; web/src/shared/lib/markdown/mentionParity.test.ts
// renders the same messages and compares.
//
//	MENTION_PARITY_UPDATE=1 go test -run TestMentionParityVectors ./internal/mention
//
// rewrites the server side after the vitest side wrote new messages.

type mentionVector struct {
	Msg    string   `json:"msg"`
	Server []string `json:"server"`
}

func TestMentionParityVectors(t *testing.T) {
	const file = "testdata/parity.json"
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var vs []mentionVector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatal(err)
	}
	if len(vs) == 0 {
		t.Fatal("no vectors")
	}
	for i := range vs {
		got := Names(vs[i].Msg)
		if got == nil {
			got = []string{}
		}
		if os.Getenv("MENTION_PARITY_UPDATE") == "1" {
			vs[i].Server = got
		} else if !reflect.DeepEqual(got, vs[i].Server) {
			t.Errorf("Names(%q) = %v, the vector file says %v (rerun with MENTION_PARITY_UPDATE=1)", vs[i].Msg, got, vs[i].Server)
		}
	}
	if os.Getenv("MENTION_PARITY_UPDATE") == "1" {
		out, err := json.MarshalIndent(vs, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
