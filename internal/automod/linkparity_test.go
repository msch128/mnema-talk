package automod

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
)

// One rule, two implementations: the server judges the links of a message
// with links(), the web client renders them with its own Markdown parser. The
// vectors in testdata/link_parity.json are messages with the links the server
// reads in each; web/src/shared/lib/markdown/linkParity.test.ts reads the same
// file and checks that every host the client would open is one judged here.
//
//	LINK_PARITY_UPDATE=1 go test -run TestLinkParityVectors ./internal/automod
//
// rewrites the server side of the file after the vitest side has written new
// messages. The deep sweep (linkParity.sweep.test.ts) runs this test with
// LINK_PARITY_IN and LINK_PARITY_OUT: messages in, the server's links out.

const linkParityFile = "testdata/link_parity.json"

type parityLink struct {
	Host   string `json:"host"`
	OK     bool   `json:"ok"`
	Opaque bool   `json:"opaque,omitempty"`
}

type parityVector struct {
	Msg    string       `json:"msg"`
	Server []parityLink `json:"server"`
}

func serverLinks(msg string) []parityLink {
	out := []parityLink{}
	for _, l := range links(msg) {
		out = append(out, parityLink{Host: l.host, OK: l.ok, Opaque: l.opaque})
	}
	return out
}

func readParityVectors(t *testing.T, path string) []parityVector {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vs []parityVector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return vs
}

func writeParityVectors(t *testing.T, path string, vs []parityVector) {
	t.Helper()
	for i := range vs {
		vs[i].Server = serverLinks(vs[i].Msg)
	}
	raw, err := json.MarshalIndent(vs, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(asciiJSON(raw), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// asciiJSON escapes every character outside printable ASCII, so the vector
// file shows the invisible characters it tests instead of hiding them.
func asciiJSON(raw []byte) []byte {
	var b strings.Builder
	for _, r := range string(raw) {
		switch {
		case r >= 0x20 && r < 0x7f || r == '\n':
			b.WriteRune(r)
		case r > 0xffff:
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, r1, r2)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return []byte(b.String())
}

func TestLinkParityVectors(t *testing.T) {
	if in, out := os.Getenv("LINK_PARITY_IN"), os.Getenv("LINK_PARITY_OUT"); in != "" && out != "" {
		writeParityVectors(t, filepath.Clean(out), readParityVectors(t, filepath.Clean(in)))
		return
	}
	vs := readParityVectors(t, linkParityFile)
	if os.Getenv("LINK_PARITY_UPDATE") == "1" {
		writeParityVectors(t, linkParityFile, vs)
		return
	}
	if len(vs) == 0 {
		t.Fatal("no vectors: the parity check would pass without a case")
	}
	for _, v := range vs {
		if got := serverLinks(v.Msg); !reflect.DeepEqual(got, v.Server) {
			t.Errorf("links(%q) = %+v, the vector file says %+v (rerun with LINK_PARITY_UPDATE=1)", v.Msg, got, v.Server)
		}
	}
}
