package linkpreview

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The fixtures in testdata are real responses recorded on 2026-10-29 with the
// fetcher's User-Agent (heads of pages stripped of scripts and unrelated tags,
// the Steam answer without its long texts). Tests never touch the network:
// fixtureTransport answers by host and path.

type fixture struct {
	status int
	ctype  string
	file   string
}

type fixtureTransport struct {
	mu     sync.Mutex
	routes map[string]fixture
	hits   []string
}

func (t *fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.hits = append(t.hits, r.URL.Host+r.URL.Path)
	fx, ok := t.routes[r.URL.Host+r.URL.Path]
	t.mu.Unlock()
	res := &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader("not found"))}
	if !ok {
		return res, nil
	}
	res.StatusCode = fx.status
	if res.StatusCode == 0 {
		res.StatusCode = http.StatusOK
	}
	res.Header.Set("Content-Type", fx.ctype)
	if fx.file != "" {
		b, err := os.ReadFile(filepath.Join("testdata", fx.file))
		if err != nil {
			return nil, err
		}
		res.Body = io.NopCloser(strings.NewReader(string(b)))
	}
	return res, nil
}

func (t *fixtureTransport) fetched(hostPath string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, h := range t.hits {
		if h == hostPath {
			return true
		}
	}
	return false
}

func fixtureFetcher(routes map[string]fixture) (*Fetcher, *fixtureTransport) {
	ft := &fixtureTransport{routes: routes}
	f := New()
	f.client.Transport = ft
	return f, ft
}

const (
	htmlType = "text/html; charset=utf-8"
	jsonType = "application/json; charset=utf-8"
)

func previewOf(t *testing.T, f *Fetcher, link string) *Preview {
	t.Helper()
	p, err := f.Preview(context.Background(), link)
	if err != nil {
		t.Fatalf("Preview(%s): %v", link, err)
	}
	return p
}

func TestYouTubeCardComesFromOEmbedAndPlaysFromTheNoCookieHost(t *testing.T) {
	f, ft := fixtureFetcher(map[string]fixture{
		"www.youtube.com/oembed": {ctype: jsonType, file: "youtube_oembed.json"},
	})
	for _, link := range []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ",
		"https://m.youtube.com/shorts/dQw4w9WgXcQ",
	} {
		p := previewOf(t, f, link)
		if p.SiteName != "YouTube" || p.Author != "Rick Astley" || p.AuthorURL != "https://www.youtube.com/@RickAstleyYT" {
			t.Errorf("%s: provider/author = %q/%q/%q", link, p.SiteName, p.Author, p.AuthorURL)
		}
		if !strings.HasPrefix(p.Title, "Rick Astley - Never Gonna Give You Up") || p.Image != "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg" {
			t.Errorf("%s: title/image = %q / %q", link, p.Title, p.Image)
		}
		if p.Embed == nil || p.Embed.Provider != "youtube" || p.Embed.URL != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ" || p.Compact {
			t.Errorf("%s: embed = %+v compact=%v", link, p.Embed, p.Compact)
		}
		if p.URL != link {
			t.Errorf("URL = %q, want the posted link %q", p.URL, link)
		}
	}
	if ft.fetched("www.youtube.com/watch") || ft.fetched("youtu.be/dQw4w9WgXcQ") {
		t.Fatal("the watch page, which may be a consent wall, must not be needed")
	}
}

func TestYouTubeStartTimeReachesThePlayer(t *testing.T) {
	f, _ := fixtureFetcher(map[string]fixture{"www.youtube.com/oembed": {ctype: jsonType, file: "youtube_oembed.json"}})
	p := previewOf(t, f, "https://youtu.be/dQw4w9WgXcQ?t=1m30s")
	if p.Embed == nil || p.Embed.URL != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=90" {
		t.Fatalf("embed = %+v", p.Embed)
	}
}

func TestYouTubeFallsBackToThePageTagsWhenOEmbedIsDown(t *testing.T) {
	f, _ := fixtureFetcher(map[string]fixture{
		"www.youtube.com/watch": {ctype: htmlType, file: "youtube_watch.html"},
	})
	p := previewOf(t, f, "https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	if p.SiteName != "YouTube" || !strings.HasPrefix(p.Title, "Rick Astley") || p.Image != "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg" {
		t.Fatalf("page fallback = %+v", p)
	}
	if p.Embed == nil || p.Embed.Provider != "youtube" || p.Compact {
		t.Fatalf("embed = %+v compact=%v", p.Embed, p.Compact)
	}
}

// A consent page has no tags at all: no card, and no error worth caching long.
func TestYouTubeConsentWallWithoutTagsGivesNoCard(t *testing.T) {
	f, _ := fixtureFetcher(map[string]fixture{"www.youtube.com/watch": {ctype: htmlType}})
	if _, err := f.Preview(context.Background(), "https://www.youtube.com/watch?v=dQw4w9WgXcQ"); !errors.Is(err, ErrNoPreview) {
		t.Fatalf("err = %v, want ErrNoPreview", err)
	}
}

func TestVimeoPageCardPlaysFromThePlayerHost(t *testing.T) {
	f, _ := fixtureFetcher(map[string]fixture{
		"vimeo.com/76979871": {ctype: htmlType, file: "vimeo_video.html"},
	})
	p := previewOf(t, f, "https://vimeo.com/76979871")
	if p.Title != "The New Vimeo Player (You Know, For Videos)" || p.Image == "" {
		t.Fatalf("card = %+v", p)
	}
	if p.Embed == nil || p.Embed.Provider != "vimeo" || p.Embed.URL != "https://player.vimeo.com/video/76979871?dnt=1" {
		t.Fatalf("embed = %+v", p.Embed)
	}
}

func TestSteamCardCarriesPriceAndRecommendations(t *testing.T) {
	f, ft := fixtureFetcher(map[string]fixture{
		"store.steampowered.com/api/appdetails": {ctype: jsonType, file: "steam_appdetails.json"},
	})
	p := previewOf(t, f, "https://store.steampowered.com/app/3572800/")
	if p.SiteName != "Steam" || p.Title != "Duo Quest" || p.Author != "1+1 Studios" || p.Color != "#1b2838" {
		t.Fatalf("card = %+v", p)
	}
	if !strings.Contains(p.Image, "3572800") || !strings.Contains(p.Image, "header.jpg") || !strings.HasPrefix(p.Description, "Your bond is your greatest weapon!") {
		t.Fatalf("image/description = %q / %q", p.Image, p.Description)
	}
	want := []Field{{Key: "price", Name: "Price", Value: "$14.99"}, {Key: "recommendations", Name: "Recommendations", Value: "141"}}
	if len(p.Fields) != 2 || p.Fields[0] != want[0] || p.Fields[1] != want[1] {
		t.Fatalf("fields = %+v", p.Fields)
	}
	if ft.fetched("store.steampowered.com/app/3572800/") {
		t.Fatal("the store page is not needed when the API answers")
	}
}

func TestSteamFallsBackToThePageWhenTheAPIFails(t *testing.T) {
	f, _ := fixtureFetcher(map[string]fixture{
		"store.steampowered.com/api/appdetails": {status: http.StatusServiceUnavailable, ctype: jsonType},
		"store.steampowered.com/app/3572800/":   {ctype: htmlType, file: "steam_store.html"},
	})
	p := previewOf(t, f, "https://store.steampowered.com/app/3572800/")
	if p.Title != "Duo Quest on Steam" || p.SiteName != "Steam" || len(p.Fields) != 0 || p.Color != "#171a21" {
		t.Fatalf("card = %+v", p)
	}
}

func TestSteamCardFormatting(t *testing.T) {
	page, _ := url.Parse("https://store.steampowered.com/app/7/")
	card := func(data string) *Preview {
		return steamCard([]byte(`{"7":{"success":true,"data":{"name":"G",`+data+`}}}`), page, "7")
	}
	cases := map[string]string{
		`"is_free":true`: "Free",
		`"is_free":false,"price_overview":{"discount_percent":25,"initial_formatted":"$20.00","final_formatted":"$15.00"}`: "-25% $15.00 (was $20.00)",
		`"is_free":false,"price_overview":{"discount_percent":0,"initial_formatted":"","final_formatted":"€9,99"}`:         "€9,99",
	}
	for data, want := range cases {
		p := card(data)
		if p == nil || len(p.Fields) != 1 || p.Fields[0].Value != want {
			t.Errorf("%s: fields = %+v, want price %q", data, p, want)
		}
	}
	if p := card(`"is_free":false,"recommendations":{"total":1234567}`); p == nil || p.Fields[0].Value != "1,234,567" {
		t.Errorf("recommendations = %+v", p)
	}
	if steamCard([]byte(`{"7":{"success":false}}`), page, "7") != nil || steamCard([]byte(`nope`), page, "7") != nil {
		t.Error("an unknown app or garbage must give no card")
	}
}

func TestGitHubCardKeepsTheWideImageAndColor(t *testing.T) {
	f, _ := fixtureFetcher(map[string]fixture{"github.com/golang/go": {ctype: htmlType, file: "github_repo.html"}})
	p := previewOf(t, f, "https://github.com/golang/go")
	if p.SiteName != "GitHub" || p.Compact || p.Color != "#1e2327" || p.Image == "" || p.Embed != nil {
		t.Fatalf("card = %+v", p)
	}
}

func TestSquareProfileImageIsCompactAndNoOEmbedFetchIsWasted(t *testing.T) {
	f, ft := fixtureFetcher(map[string]fixture{
		"x.com/jack/status/20":  {ctype: htmlType, file: "x_status.html"},
		"publish.x.com/oembed":  {ctype: jsonType, file: "x_oembed.json"},
		"publish.x.com/oembed/": {ctype: jsonType, file: "x_oembed.json"},
	})
	p := previewOf(t, f, "https://x.com/jack/status/20")
	if p.Title != "jack (@jack) on X" || p.Description != "just setting up my twttr" || !p.Compact {
		t.Fatalf("card = %+v", p)
	}
	if ft.fetched("publish.x.com/oembed") {
		t.Fatal("a complete page must not trigger a second fetch")
	}
}

func TestThemeColorOfPlainPages(t *testing.T) {
	f, _ := fixtureFetcher(map[string]fixture{"www.bbc.com/news": {ctype: htmlType, file: "bbc_news.html"}})
	p := previewOf(t, f, "https://www.bbc.com/news")
	if p.Color != "#ffffff" || p.Image != "" || !strings.HasPrefix(p.Title, "BBC News") {
		t.Fatalf("card = %+v", p)
	}
}

func TestThemeColorOnlyAcceptsHex(t *testing.T) {
	for in, want := range map[string]string{
		"#FFF": "#ffffff", "#1E2327": "#1e2327", " #0a0a0a ": "#0a0a0a",
		"red": "", "rgba(0,0,0,.9)": "", "#12": "", "#12345": "", "url(x)": "", "#ggg": "",
	} {
		if got := themeColor(in); got != want {
			t.Errorf("themeColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOEmbedDiscoveryFillsGapsButNeverRendersItsHTML(t *testing.T) {
	f, ft := fixtureFetcher(map[string]fixture{
		"blog.example/post": {ctype: htmlType, file: "oembed_gap.html"},
		"embed.example/o":   {ctype: jsonType, file: "oembed_gap.json"},
	})
	p := previewOf(t, f, "https://blog.example/post")
	if !ft.fetched("embed.example/o") {
		t.Fatal("the advertised endpoint was not asked")
	}
	if p.Title != "Page title" || p.Author != "A. Writer" || p.AuthorURL != "https://blog.example/writer" ||
		p.Image != "https://blog.example/thumb.png" || p.SiteName != "Example Blog" {
		t.Fatalf("card = %+v", p)
	}
	if strings.Contains(p.Description, "<") || p.Embed != nil {
		t.Fatalf("html from oEmbed leaked: %+v", p)
	}
}

func TestOEmbedEndpointsOnOddPortsAreNotFollowed(t *testing.T) {
	f, ft := fixtureFetcher(map[string]fixture{
		"blog.example/odd": {ctype: htmlType, file: "oembed_oddport.html"},
	})
	p := previewOf(t, f, "https://blog.example/odd")
	if p.Title != "Odd" || p.Author != "" {
		t.Fatalf("card = %+v", p)
	}
	for _, h := range ft.hits {
		if strings.Contains(h, "internal") {
			t.Fatalf("followed %s", h)
		}
	}
}

func TestPageSuppliedVideoNeverBecomesAnEmbed(t *testing.T) {
	f, _ := fixtureFetcher(map[string]fixture{"evil.example/v": {ctype: htmlType, file: "evil_video.html"}})
	p := previewOf(t, f, "https://evil.example/v")
	if p.Embed != nil || p.Title != "Totally a video" {
		t.Fatalf("card = %+v", p)
	}
}

func TestRecognizeVideoOnlyAcceptsTheRealHostsAndIDs(t *testing.T) {
	ok := map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=5":      "youtube:dQw4w9WgXcQ",
		"https://youtube.com/embed/dQw4w9WgXcQ":                "youtube:dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ":        "youtube:dQw4w9WgXcQ",
		"https://youtu.be/dQw4w9WgXcQ":                         "youtube:dQw4w9WgXcQ",
		"https://www.youtube.com/live/dQw4w9WgXcQ":             "youtube:dQw4w9WgXcQ",
		"https://vimeo.com/76979871":                           "vimeo:76979871",
		"https://vimeo.com/76979871/8272103f6e":                "vimeo:76979871",
		"https://vimeo.com/channels/staffpicks/76979871":       "vimeo:76979871",
		"https://player.vimeo.com/video/76979871?h=8272103f6e": "vimeo:76979871",
	}
	for raw, want := range ok {
		u, _ := url.Parse(raw)
		v := recognizeVideo(u)
		if v == nil || v.provider+":"+v.id != want {
			t.Errorf("%s = %+v, want %s", raw, v, want)
		}
	}
	for _, raw := range []string{
		"https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ",
		"https://evilyoutube.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com/watch?v=short",
		"https://www.youtube.com/watch?v=dQw4w9WgXc%22",
		"https://www.youtube.com/channel/UCuAXFkgsw1L7xaCfnd5JJOw",
		"https://youtu.be/",
		"https://vimeo.com/abc",
		"https://vimeo.com/76979871/not-a-hash!",
		"https://vimeo.com/76979871/a/b",
		"https://notvimeo.com/76979871",
		"https://player.vimeo.com/video/76979871/extra",
	} {
		u, _ := url.Parse(raw)
		if v := recognizeVideo(u); v != nil {
			t.Errorf("%s must not be recognised, got %+v", raw, v)
		}
	}
}

func TestYouTubeStartParsing(t *testing.T) {
	for in, want := range map[string]int{"90": 90, "90s": 90, "1m30s": 90, "1h2m3s": 3723, "": 0, "abc": 0, "99999h": 0, "-5": 0} {
		u, _ := url.Parse("https://youtu.be/dQw4w9WgXcQ?t=" + in)
		if got := youtubeStart(u); got != want {
			t.Errorf("t=%q: %d, want %d", in, got, want)
		}
	}
}

func TestEmbedURLsMatchTheClientAllowlist(t *testing.T) {
	yt := (&video{provider: "youtube", id: "dQw4w9WgXcQ", start: 3}).embed().URL
	vm := (&video{provider: "vimeo", id: "76979871", hash: "8272103f6e"}).embed().URL
	if yt != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=3" || vm != "https://player.vimeo.com/video/76979871?dnt=1&h=8272103f6e" {
		t.Fatalf("embed urls: %s %s", yt, vm)
	}
}

func TestTwitterCardFallsBackWhenNoOpenGraph(t *testing.T) {
	base, _ := url.Parse("https://x.example/p")
	p := parseHTML([]byte(`<head><title>T</title>
<meta name="twitter:title" content="Tw title"><meta name="twitter:description" content="Tw desc">
<meta name="twitter:image:src" content="/i.png"><meta name="twitter:card" content="summary">
<meta name="author" content="Some One"><meta property="article:author" content="https://x.example/profile">
<link rel="alternate" type="text/xml+oembed" href="/xml"><link rel="alternate" type="application/json+oembed" href="/o.json"></head>`), "text/html", base)
	if p.Title != "Tw title" || p.Description != "Tw desc" || p.Image != "https://x.example/i.png" || !p.Compact || p.Author != "Some One" || p.oembed != "/o.json" {
		t.Fatalf("parsed = %+v", p)
	}
}
