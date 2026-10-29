package linkpreview

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Videos the client may play inline. The embed URL is built here from the id
// found in the link itself, for exactly these providers and hosts; nothing
// from a fetched page ever becomes an iframe address. The web client checks
// the same shapes again (web/src/lib/videoEmbed.ts) and the CSP frame-src
// allows only these two origins.
const (
	embedYouTube = "youtube"
	embedVimeo   = "vimeo"
)

var (
	youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	vimeoID   = regexp.MustCompile(`^[0-9]{1,12}$`)
	vimeoHash = regexp.MustCompile(`^[0-9a-f]{6,32}$`)
	startTime = regexp.MustCompile(`^(?:(\d{1,5})h)?(?:(\d{1,5})m)?(?:(\d{1,5})s?)?$`)
)

// video is a recognised YouTube or Vimeo link.
type video struct {
	provider string
	id       string
	hash     string // Vimeo's privacy hash of unlisted videos
	start    int    // seconds, YouTube
}

// recognizeVideo reads u as a YouTube or Vimeo video link, or returns nil.
func recognizeVideo(u *url.URL) *video {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		return youtubeFromPath(u, parts)
	case "youtu.be":
		if len(parts) >= 1 && youtubeID.MatchString(parts[0]) {
			return &video{provider: embedYouTube, id: parts[0], start: youtubeStart(u)}
		}
	case "vimeo.com":
		return vimeoFromPath(parts)
	case "player.vimeo.com":
		if len(parts) == 2 && parts[0] == "video" && vimeoID.MatchString(parts[1]) {
			return &video{provider: embedVimeo, id: parts[1], hash: vimeoQueryHash(u)}
		}
	}
	return nil
}

func youtubeFromPath(u *url.URL, parts []string) *video {
	id := ""
	switch {
	case len(parts) == 1 && parts[0] == "watch":
		id = u.Query().Get("v")
	case len(parts) == 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live" || parts[0] == "v"):
		id = parts[1]
	}
	if !youtubeID.MatchString(id) {
		return nil
	}
	return &video{provider: embedYouTube, id: id, start: youtubeStart(u)}
}

// vimeoFromPath reads /ID, /ID/HASH and /channels/NAME/ID.
func vimeoFromPath(parts []string) *video {
	if len(parts) == 3 && parts[0] == "channels" {
		parts = parts[2:]
	}
	if len(parts) == 0 || len(parts) > 2 || !vimeoID.MatchString(parts[0]) {
		return nil
	}
	v := &video{provider: embedVimeo, id: parts[0]}
	if len(parts) == 2 {
		if !vimeoHash.MatchString(parts[1]) {
			return nil
		}
		v.hash = parts[1]
	}
	return v
}

func vimeoQueryHash(u *url.URL) string {
	if h := u.Query().Get("h"); vimeoHash.MatchString(h) {
		return h
	}
	return ""
}

// youtubeStart reads t= or start= as seconds ("90", "90s", "1m30s").
func youtubeStart(u *url.URL) int {
	q := u.Query()
	raw := strings.ToLower(q.Get("t"))
	if raw == "" {
		raw = strings.ToLower(q.Get("start"))
	}
	m := startTime.FindStringSubmatch(raw)
	if raw == "" || m == nil {
		return 0
	}
	n := func(s string) int { v, _ := strconv.Atoi(s); return v }
	total := n(m[1])*3600 + n(m[2])*60 + n(m[3])
	if total > 24*3600 {
		return 0
	}
	return total
}

// embed is the player address for the client: the privacy-enhanced YouTube
// host (no cookies until the member plays) or Vimeo's player with do-not-track.
func (v *video) embed() *Embed {
	switch v.provider {
	case embedYouTube:
		e := "https://www.youtube-nocookie.com/embed/" + v.id
		if v.start > 0 {
			e += "?start=" + strconv.Itoa(v.start)
		}
		return &Embed{Provider: embedYouTube, URL: e}
	case embedVimeo:
		e := "https://player.vimeo.com/video/" + v.id + "?dnt=1"
		if v.hash != "" {
			e += "&h=" + v.hash
		}
		return &Embed{Provider: embedVimeo, URL: e}
	}
	return nil
}

// oembedEndpoint is the provider's own oEmbed address. It answers without
// the consent page that YouTube may put in front of the watch page for EU
// visitors, which would leave a page fetch without any tags.
func (v *video) oembedEndpoint() *url.URL {
	var base, page string
	switch v.provider {
	case embedYouTube:
		base, page = "https://www.youtube.com/oembed", "https://www.youtube.com/watch?v="+v.id
	case embedVimeo:
		base, page = "https://vimeo.com/api/oembed.json", "https://vimeo.com/"+v.id
		if v.hash != "" {
			page += "/" + v.hash
		}
	}
	u, err := url.Parse(base + "?format=json&url=" + url.QueryEscape(page))
	if err != nil {
		return nil
	}
	return u
}

// videoPreview builds the card from the provider's oEmbed answer alone. It
// returns nil (and no error) when the provider does not answer usefully, so
// the caller falls back to the page's own tags.
func (f *Fetcher) videoPreview(ctx context.Context, u *url.URL, v *video) (*Preview, error) {
	endpoint := v.oembedEndpoint()
	if endpoint == nil {
		return nil, nil
	}
	o, err := f.fetchOEmbed(ctx, endpoint)
	if isBusy(err) {
		return nil, err
	}
	if err != nil || strings.TrimSpace(o.Title) == "" {
		return nil, nil
	}
	p := &Preview{URL: u.String(), Embed: v.embed()}
	o.fill(p, endpoint)
	return p, nil
}
