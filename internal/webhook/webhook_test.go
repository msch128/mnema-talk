package webhook

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestIsExecute(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"POST", "/api/webhooks/id/token", true},
		{"POST", "/api/webhooks/id/token/", false},
		{"POST", "/api/webhooks/id/", false},
		{"POST", "/api/webhooks//token", false},
		{"POST", "/api/webhooks/id", false},
		{"POST", "/api/webhooks/id/token/extra", false},
		{"GET", "/api/webhooks/id/token", false},
		{"POST", "/api/admin/webhooks/id", false},
		{"POST", "/api/channels/x/webhooks/id/token", false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if got := IsExecute(r); got != tc.want {
			t.Errorf("%s %s = %v", tc.method, tc.path, got)
		}
	}
}

func TestTokens(t *testing.T) {
	tok, hash := newToken()
	if len(tok) != 43 {
		t.Fatalf("token %q", tok)
	}
	rec := &record{tokenHash: hash}
	if !rec.tokenMatches(tok) || rec.tokenMatches(tok+"x") || rec.tokenMatches("") {
		t.Fatal("token comparison")
	}
	other, _ := newToken()
	if other == tok {
		t.Fatal("tokens repeat")
	}
}

func TestNamesAndBotNames(t *testing.T) {
	if got, err := cleanName("name", "  Bot  "); err != nil || got != "Bot" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"", "   ", "a\nb", "a\x7fb", strings.Repeat("x", MaxNameLen+1), "a\x00b", "\xff"} {
		if _, err := cleanName("name", bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if got := botDisplayName(strings.Repeat("ä", 40)); len([]rune(got)) != botNameLen {
		t.Fatalf("bot name %q", got)
	}
	if botDisplayName("short") != "short" {
		t.Fatal("short names stay")
	}
}

func embeds(t *testing.T, raw string) []inEmbed {
	t.Helper()
	var in []inEmbed
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestSanitizeEmbeds(t *testing.T) {
	got, err := sanitizeEmbeds(embeds(t, `[
		{"title":" T ","description":"d","url":"https://a.example/x","color":255,"timestamp":"2026-01-02T03:04:05+02:00",
		 "footer":{"text":"f","icon_url":"https://a.example/i"},"image":{"url":"https://a.example/i.png"},
		 "thumbnail":{"url":"https://a.example/t.png"},"author":{"name":"a","url":"https://a.example"},
		 "fields":[{"name":"n","value":"v","inline":true}],"type":"rich"},
		{}]`))
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	e := got[0]
	if e.Title != "T" || e.Timestamp != "2026-01-02T01:04:05Z" || e.Footer.Text != "f" || e.Image.URL == "" ||
		e.Thumbnail.URL == "" || e.Author.Name != "a" || !e.Fields[0].Inline || *e.Color != 255 {
		t.Fatalf("embed %+v", e)
	}

	big := func(n int) string { return strings.Repeat("x", n) }
	bad := map[string]string{
		"title long":   `[{"title":"` + big(257) + `"}]`,
		"desc long":    `[{"description":"` + big(4097) + `"}]`,
		"control":      `[{"title":"a\u0001b"}]`,
		"url scheme":   `[{"title":"t","url":"file:///etc/passwd"}]`,
		"url no host":  `[{"title":"t","url":"https://"}]`,
		"url space":    `[{"title":"t","url":"https://a.example/a b"}]`,
		"url long":     `[{"title":"t","url":"https://a.example/` + big(2048) + `"}]`,
		"url garbage":  `[{"title":"t","url":"http://[::1"}]`,
		"color high":   `[{"title":"t","color":16777216}]`,
		"color low":    `[{"title":"t","color":-1}]`,
		"timestamp":    `[{"title":"t","timestamp":"nope"}]`,
		"footer long":  `[{"footer":{"text":"` + big(2049) + `"}}]`,
		"image":        `[{"image":{"url":"javascript:1"}}]`,
		"thumbnail":    `[{"thumbnail":{"url":"javascript:1"}}]`,
		"author name":  `[{"author":{"name":"` + big(257) + `"}}]`,
		"author url":   `[{"author":{"name":"a","url":"javascript:1"}}]`,
		"field empty":  `[{"fields":[{"name":"n","value":""}]}]`,
		"field name":   `[{"fields":[{"name":"` + big(257) + `","value":"v"}]}]`,
		"field value":  `[{"fields":[{"name":"n","value":"` + big(1025) + `"}]}]`,
		"field count":  `[{"fields":[` + strings.TrimSuffix(strings.Repeat(`{"name":"n","value":"v"},`, 26), ",") + `]}]`,
		"text invalid": "[{\"title\":\"\xff\"}]",
	}
	for name, raw := range bad {
		var in []inEmbed
		// Invalid UTF-8 survives encoding/json as U+FFFD; build it by hand.
		if name == "text invalid" {
			in = []inEmbed{{Title: "\xff"}}
		} else {
			in = embeds(t, raw)
		}
		if _, err := sanitizeEmbeds(in); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestEmbedLimits(t *testing.T) {
	if _, err := sanitizeEmbeds(make([]inEmbed, maxEmbeds+1)); err == nil {
		t.Fatal("11 embeds accepted")
	}
	// Total text over 6000 characters across embeds.
	e := inEmbed{Description: strings.Repeat("x", 4000)}
	if _, err := sanitizeEmbeds([]inEmbed{e, e}); err == nil {
		t.Fatal("8000 characters accepted")
	}
}

func TestWriteFailure(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   int
	}{
		{formError("x"), 400, codeInvalidFormBody},
		{emptyMessage{}, 400, codeEmptyMessage},
		{unknownThread{}, 404, codeUnknownChannel},
		{httpx.ErrInvalidInput("x"), 400, codeInvalidFormBody},
		{httpx.ErrPayloadTooLarge("x"), 413, codeEntityTooLarge},
		{httpx.ErrUnsupportedMediaType("x"), 415, codeUnsupportedMedia},
		{httpx.ErrUnavailable("x"), 503, 0},
		{errors.New("db exploded"), 500, 0},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		writeFailure(w, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":`+strconv.Itoa(tc.code)) {
			t.Errorf("%v: %d %s", tc.err, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "exploded") {
			t.Error("internal error leaked")
		}
	}
}
