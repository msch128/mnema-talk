package chat

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Discord's embed limits, which Mnema Talk applies as well.
const (
	MaxEmbeds      = 10
	maxEmbedFields = 25
	maxEmbedTotal  = 6000
	maxURLLen      = 2048
)

// InEmbed is a Discord embed as senders write it. Fields Mnema Talk does not
// use (type, video, provider, icon URLs, proxy URLs) are accepted and dropped.
type InEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Timestamp   string `json:"timestamp"`
	Color       *int   `json:"color"`
	Footer      *struct {
		Text string `json:"text"`
	} `json:"footer"`
	Image *struct {
		URL string `json:"url"`
	} `json:"image"`
	Thumbnail *struct {
		URL string `json:"url"`
	} `json:"thumbnail"`
	Author *struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"author"`
	Fields []struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Inline bool   `json:"inline"`
	} `json:"fields"`
}

// FormError is a validation failure of the payload (Discord: "Invalid Form
// Body", code 50035).
type FormError string

func (e FormError) Error() string { return string(e) }

// text validates one embed text: valid UTF-8, no control characters except
// line breaks and tabs, at most max characters. Surrounding space is trimmed.
func embedText(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) {
		return "", FormError(field + " is not valid UTF-8")
	}
	if utf8.RuneCountInString(s) > max {
		return "", FormError(fmt.Sprintf("%s must be at most %d characters", field, max))
	}
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t' && r != '\r') || r == 0x7f {
			return "", FormError(field + " contains control characters")
		}
	}
	return s, nil
}

// link validates a link: absolute http(s) with a host, at most 2048 bytes.
// Embed links are only ever shown as links, never loaded.
func embedLink(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if len(s) > maxURLLen {
		return "", FormError(field + " is too long")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", FormError(field + " must be an http or https URL")
	}
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return "", FormError(field + " must be an http or https URL")
		}
	}
	return s, nil
}

// SanitizeEmbeds validates and converts the embeds of a payload. Embeds with
// nothing to show are dropped.
func SanitizeEmbeds(in []InEmbed) ([]Embed, error) {
	if len(in) > MaxEmbeds {
		return nil, FormError(fmt.Sprintf("embeds: at most %d embeds are allowed", MaxEmbeds))
	}
	total := 0
	out := make([]Embed, 0, len(in))
	for i, e := range in {
		em, n, err := sanitizeOneEmbed(fmt.Sprintf("embeds[%d]", i), e)
		if err != nil {
			return nil, err
		}
		if n == 0 && em.Image == nil && em.Thumbnail == nil {
			continue
		}
		total += n
		out = append(out, em)
	}
	if total > maxEmbedTotal {
		return nil, FormError(fmt.Sprintf("embeds: the text of all embeds must be at most %d characters", maxEmbedTotal))
	}
	return out, nil
}

// embedBuilder validates one embed field by field and counts the characters
// that add up to the per-message text budget.
type embedBuilder struct {
	at    string
	em    Embed
	chars int
}

func (b *embedBuilder) count(s string) { b.chars += utf8.RuneCountInString(s) }

func sanitizeOneEmbed(at string, e InEmbed) (Embed, int, error) {
	b := &embedBuilder{at: at}
	steps := []func(InEmbed) error{
		b.heading, b.color, b.timestamp, b.footer, b.image, b.thumbnail, b.author, b.fields,
	}
	for _, step := range steps {
		if err := step(e); err != nil {
			return b.em, 0, err
		}
	}
	return b.em, b.chars, nil
}

func (b *embedBuilder) heading(e InEmbed) (err error) {
	if b.em.Title, err = embedText(b.at+".title", e.Title, 256); err != nil {
		return err
	}
	if b.em.Description, err = embedText(b.at+".description", e.Description, 4096); err != nil {
		return err
	}
	b.count(b.em.Title)
	b.count(b.em.Description)
	b.em.URL, err = embedLink(b.at+".url", e.URL)
	return err
}

func (b *embedBuilder) color(e InEmbed) error {
	if e.Color == nil {
		return nil
	}
	if *e.Color < 0 || *e.Color > 0xFFFFFF {
		return FormError(b.at + ".color must be between 0 and 16777215")
	}
	c := *e.Color
	b.em.Color = &c
	return nil
}

func (b *embedBuilder) timestamp(e InEmbed) error {
	ts := strings.TrimSpace(e.Timestamp)
	if ts == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return FormError(b.at + ".timestamp must be an ISO 8601 timestamp")
	}
	b.em.Timestamp = t.UTC().Format(time.RFC3339)
	return nil
}

func (b *embedBuilder) footer(e InEmbed) error {
	if e.Footer == nil {
		return nil
	}
	t, err := embedText(b.at+".footer.text", e.Footer.Text, 2048)
	if err != nil || t == "" {
		return err
	}
	b.em.Footer = &EmbedFooter{Text: t}
	b.count(t)
	return nil
}

// mediaLink validates the URL of an image or thumbnail; an empty URL yields no media.
func (b *embedBuilder) mediaLink(field, raw string) (*EmbedMedia, error) {
	u, err := embedLink(b.at+field, raw)
	if err != nil || u == "" {
		return nil, err
	}
	return &EmbedMedia{URL: u}, nil
}

func (b *embedBuilder) image(e InEmbed) (err error) {
	if e.Image == nil {
		return nil
	}
	b.em.Image, err = b.mediaLink(".image.url", e.Image.URL)
	return err
}

func (b *embedBuilder) thumbnail(e InEmbed) (err error) {
	if e.Thumbnail == nil {
		return nil
	}
	b.em.Thumbnail, err = b.mediaLink(".thumbnail.url", e.Thumbnail.URL)
	return err
}

func (b *embedBuilder) author(e InEmbed) error {
	if e.Author == nil {
		return nil
	}
	name, err := embedText(b.at+".author.name", e.Author.Name, 256)
	if err != nil {
		return err
	}
	u, err := embedLink(b.at+".author.url", e.Author.URL)
	if err != nil {
		return err
	}
	if name != "" {
		b.em.Author = &EmbedAuthor{Name: name, URL: u}
		b.count(name)
	}
	return nil
}

func (b *embedBuilder) fields(e InEmbed) error {
	if len(e.Fields) > maxEmbedFields {
		return FormError(fmt.Sprintf("%s.fields: at most %d fields are allowed", b.at, maxEmbedFields))
	}
	for j, f := range e.Fields {
		fat := fmt.Sprintf("%s.fields[%d]", b.at, j)
		name, err := embedText(fat+".name", f.Name, 256)
		if err != nil {
			return err
		}
		value, err := embedText(fat+".value", f.Value, 1024)
		if err != nil {
			return err
		}
		if name == "" || value == "" {
			return FormError(fat + " needs a name and a value")
		}
		b.em.Fields = append(b.em.Fields, EmbedField{Name: name, Value: value, Inline: f.Inline})
		b.count(name)
		b.count(value)
	}
	return nil
}
