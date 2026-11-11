package eventstest

import (
	"strings"
	"testing"
)

// failures stands in for the test whose golden file is checked and keeps what
// it reported.
type failures struct {
	testing.TB
	messages []string
}

func (f *failures) Helper() {}

func (f *failures) Errorf(format string, args ...any) { f.messages = append(f.messages, format) }

func TestNormalizeKeepsEverythingButWhatChangesBetweenRuns(t *testing.T) {
	frame := `{"type":"message_create","payload":{"id":"0d9e2a44-63a8-4c0e-9c56-0d0f4a7c8a11","number":-7,` +
		`"user_id":"7b1c2f0e-1111-4222-8333-444455556666","reply_to":"0d9e2a44-63a8-4c0e-9c56-0d0f4a7c8a11",` +
		`"username":"wh-0123456789abcdef","parent_id":null,"created_at":"2026-11-11T15:04:05.123456Z"}}`
	want := `{"type":"message_create","payload":{"id":"<id-1>","number":<n>,"user_id":"<id-2>","reply_to":"<id-1>",` +
		`"username":"wh-<hex>","parent_id":null,"created_at":"<time>"}}`
	if got := Normalize([]byte(frame)); got != want {
		t.Fatalf("normalized frame:\n got:  %s\n want: %s", got, want)
	}
}

func TestGoldenFileIsWrittenOnRequestAndComparedOtherwise(t *testing.T) {
	t.Chdir(t.TempDir())
	record := func(tb testing.TB, payload string) {
		g := Open(tb, "sample")
		g.Step("one step", Frame{Site: "internal/x.Publish", Wire: []byte(`{"type":"x","payload":` + payload + `}`)})
		g.Check()
	}
	t.Setenv("UPDATE_EVENT_GOLDEN", "1")
	record(t, `{"a":1,"b":null}`)
	t.Setenv("UPDATE_EVENT_GOLDEN", "")
	record(t, `{"a":1,"b":null}`)

	for name, payload := range map[string]string{"key order": `{"b":null,"a":1}`, "omitted field": `{"a":1}`} {
		changed := &failures{TB: t}
		record(changed, payload)
		if len(changed.messages) != 1 || !strings.Contains(changed.messages[0], "public API") {
			t.Errorf("%s: a changed frame must fail the test, got %v", name, changed.messages)
		}
	}
	silent := &failures{TB: t}
	Open(silent, "sample").Step("nothing happened")
	if len(silent.messages) != 1 {
		t.Errorf("a step without a frame must fail the test, got %v", silent.messages)
	}
}
