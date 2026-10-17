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

func sanitizeOneEmbed(at string, e InEmbed) (Embed, int, error) {
	var em Embed
	var err error
	n := 0
	count := func(s string) { n += utf8.RuneCountInString(s) }

	if em.Title, err = embedText(at+".title", e.Title, 256); err != nil {
		return em, 0, err
	}
	if em.Description, err = embedText(at+".description", e.Description, 4096); err != nil {
		return em, 0, err
	}
	count(em.Title)
	count(em.Description)
	if em.URL, err = embedLink(at+".url", e.URL); err != nil {
		return em, 0, err
	}
	if e.Color != nil {
		if *e.Color < 0 || *e.Color > 0xFFFFFF {
			return em, 0, FormError(at + ".color must be between 0 and 16777215")
		}
		c := *e.Color
		em.Color = &c
	}
	if ts := strings.TrimSpace(e.Timestamp); ts != "" {
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			return em, 0, FormError(at + ".timestamp must be an ISO 8601 timestamp")
		}
		em.Timestamp = t.UTC().Format(time.RFC3339)
	}
	if e.Footer != nil {
		t, err := embedText(at+".footer.text", e.Footer.Text, 2048)
		if err != nil {
			return em, 0, err
		}
		if t != "" {
			em.Footer = &EmbedFooter{Text: t}
			count(t)
		}
	}
	if e.Image != nil {
		u, err := embedLink(at+".image.url", e.Image.URL)
		if err != nil {
			return em, 0, err
		}
		if u != "" {
			em.Image = &EmbedMedia{URL: u}
		}
	}
	if e.Thumbnail != nil {
		u, err := embedLink(at+".thumbnail.url", e.Thumbnail.URL)
		if err != nil {
			return em, 0, err
		}
		if u != "" {
			em.Thumbnail = &EmbedMedia{URL: u}
		}
	}
	if e.Author != nil {
		name, err := embedText(at+".author.name", e.Author.Name, 256)
		if err != nil {
			return em, 0, err
		}
		u, err := embedLink(at+".author.url", e.Author.URL)
		if err != nil {
			return em, 0, err
		}
		if name != "" {
			em.Author = &EmbedAuthor{Name: name, URL: u}
			count(name)
		}
	}
	if len(e.Fields) > maxEmbedFields {
		return em, 0, FormError(fmt.Sprintf("%s.fields: at most %d fields are allowed", at, maxEmbedFields))
	}
	for j, f := range e.Fields {
		fat := fmt.Sprintf("%s.fields[%d]", at, j)
		name, err := embedText(fat+".name", f.Name, 256)
		if err != nil {
			return em, 0, err
		}
		value, err := embedText(fat+".value", f.Value, 1024)
		if err != nil {
			return em, 0, err
		}
		if name == "" || value == "" {
			return em, 0, FormError(fat + " needs a name and a value")
		}
		em.Fields = append(em.Fields, EmbedField{Name: name, Value: value, Inline: f.Inline})
		count(name)
		count(value)
	}
	return em, n, nil
}
