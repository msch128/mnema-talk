package linkpreview

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// maxOEmbedBytes bounds an oEmbed answer; real ones are a few hundred bytes
// plus an HTML snippet that is never used.
const maxOEmbedBytes = 64 << 10

// oEmbed is the part of an oEmbed 1.0 answer a card uses. The "html" snippet
// is deliberately not decoded: markup from a third party is never rendered.
type oEmbed struct {
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	AuthorURL    string `json:"author_url"`
	ProviderName string `json:"provider_name"`
	ThumbnailURL string `json:"thumbnail_url"`
}

// fetchOEmbed asks endpoint (an absolute oEmbed URL, fetched through the same
// guarded client as pages) about the page. Only a JSON answer counts.
func (f *Fetcher) fetchOEmbed(ctx context.Context, endpoint *url.URL) (*oEmbed, error) {
	if err := f.checkURL(endpoint); err != nil {
		return nil, err
	}
	body, _, _, err := f.get(ctx, endpoint, "application/json", maxOEmbedBytes)
	if err != nil {
		return nil, err
	}
	var o oEmbed
	if err := json.Unmarshal(body, &o); err != nil {
		return nil, ErrNoPreview
	}
	return &o, nil
}

// fill adds what o knows to the gaps of p; the page's own tags win.
func (o *oEmbed) fill(p *Preview, base *url.URL) {
	if p.Title == "" {
		p.Title = clip(o.Title, maxTitleRunes)
	}
	if p.SiteName == "" {
		p.SiteName = clip(o.ProviderName, 80)
	}
	if p.Author == "" {
		p.Author = clip(o.AuthorName, 100)
		p.AuthorURL = absoluteHTTP(base, o.AuthorURL)
	}
	if p.Image == "" {
		p.Image = absoluteHTTP(base, o.ThumbnailURL)
	}
}

// needsOEmbed reports whether the page left a gap its oEmbed endpoint could
// fill. That keeps the common case, a complete Open Graph page, at one fetch.
func needsOEmbed(p *Preview) bool {
	return p.oembed != "" && (p.Title == "" || p.Image == "" || (p.Embed != nil && p.Author == ""))
}

// enrichFromOEmbed follows the endpoint the page advertised. It is best
// effort: any failure except a busy fetcher leaves the card as it is.
func (f *Fetcher) enrichFromOEmbed(ctx context.Context, p *Preview, page *url.URL) error {
	if !needsOEmbed(p) {
		return nil
	}
	endpoint, err := page.Parse(strings.TrimSpace(p.oembed))
	if err != nil {
		return nil
	}
	o, err := f.fetchOEmbed(ctx, endpoint)
	if isBusy(err) {
		return err
	}
	if err == nil {
		o.fill(p, page)
	}
	return nil
}
