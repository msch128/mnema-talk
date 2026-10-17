package webhook

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/msch128/mnema-talk/internal/chat"
)

// Discord's embed limits, which Mnema Talk applies as well.
const (
	maxEmbeds      = 10
	maxEmbedFields = 25
	maxEmbedTotal  = 6000
	maxURLLen      = 2048
)

// inEmbed is a Discord embed as senders write it. Fields Mnema Talk does not
// use (type, video, provider, icon URLs, proxy URLs) are accepted and dropped.
type inEmbed struct {
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

// formError is a validation failure of the payload (Discord: "Invalid Form
// Body", code 50035).
type formError string

func (e formError) Error() string { return string(e) }

// text validates one embed text: valid UTF-8, no control characters except
// line breaks and tabs, at most max characters. Surrounding space is trimmed.
func text(field, s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if !utf8.ValidString(s) {
		return "", formError(field + " is not valid UTF-8")
	}
	if utf8.RuneCountInString(s) > max {
		return "", formError(fmt.Sprintf("%s must be at most %d characters", field, max))
	}
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t' && r != '\r') || r == 0x7f {
			return "", formError(field + " contains control characters")
		}
	}
	return s, nil
}

// link validates a link: absolute http(s) with a host, at most 2048 bytes.
// Embed links are only ever shown as links, never loaded.
func link(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if len(s) > maxURLLen {
		return "", formError(field + " is too long")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", formError(field + " must be an http or https URL")
	}
	for _, r := range s {
		if r <= 0x20 || r == 0x7f {
			return "", formError(field + " must be an http or https URL")
		}
	}
	return s, nil
}

// sanitizeEmbeds validates and converts the embeds of a payload. Embeds with
// nothing to show are dropped.
func sanitizeEmbeds(in []inEmbed) ([]chat.Embed, error) {
	if len(in) > maxEmbeds {
		return nil, formError(fmt.Sprintf("embeds: at most %d embeds are allowed", maxEmbeds))
	}
	total := 0
	out := make([]chat.Embed, 0, len(in))
	for i, e := range in {
		em, n, err := sanitizeEmbed(fmt.Sprintf("embeds[%d]", i), e)
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
		return nil, formError(fmt.Sprintf("embeds: the text of all embeds must be at most %d characters", maxEmbedTotal))
	}
	return out, nil
}

func sanitizeEmbed(at string, e inEmbed) (chat.Embed, int, error) {
	var em chat.Embed
	var err error
	n := 0
	count := func(s string) { n += utf8.RuneCountInString(s) }

	if em.Title, err = text(at+".title", e.Title, 256); err != nil {
		return em, 0, err
	}
	if em.Description, err = text(at+".description", e.Description, 4096); err != nil {
		return em, 0, err
	}
	count(em.Title)
	count(em.Description)
	if em.URL, err = link(at+".url", e.URL); err != nil {
		return em, 0, err
	}
	if e.Color != nil {
		if *e.Color < 0 || *e.Color > 0xFFFFFF {
			return em, 0, formError(at + ".color must be between 0 and 16777215")
		}
		c := *e.Color
		em.Color = &c
	}
	if ts := strings.TrimSpace(e.Timestamp); ts != "" {
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			return em, 0, formError(at + ".timestamp must be an ISO 8601 timestamp")
		}
		em.Timestamp = t.UTC().Format(time.RFC3339)
	}
	if e.Footer != nil {
		t, err := text(at+".footer.text", e.Footer.Text, 2048)
		if err != nil {
			return em, 0, err
		}
		if t != "" {
			em.Footer = &chat.EmbedFooter{Text: t}
			count(t)
		}
	}
	if e.Image != nil {
		u, err := link(at+".image.url", e.Image.URL)
		if err != nil {
			return em, 0, err
		}
		if u != "" {
			em.Image = &chat.EmbedMedia{URL: u}
		}
	}
	if e.Thumbnail != nil {
		u, err := link(at+".thumbnail.url", e.Thumbnail.URL)
		if err != nil {
			return em, 0, err
		}
		if u != "" {
			em.Thumbnail = &chat.EmbedMedia{URL: u}
		}
	}
	if e.Author != nil {
		name, err := text(at+".author.name", e.Author.Name, 256)
		if err != nil {
			return em, 0, err
		}
		u, err := link(at+".author.url", e.Author.URL)
		if err != nil {
			return em, 0, err
		}
		if name != "" {
			em.Author = &chat.EmbedAuthor{Name: name, URL: u}
			count(name)
		}
	}
	if len(e.Fields) > maxEmbedFields {
		return em, 0, formError(fmt.Sprintf("%s.fields: at most %d fields are allowed", at, maxEmbedFields))
	}
	for j, f := range e.Fields {
		fat := fmt.Sprintf("%s.fields[%d]", at, j)
		name, err := text(fat+".name", f.Name, 256)
		if err != nil {
			return em, 0, err
		}
		value, err := text(fat+".value", f.Value, 1024)
		if err != nil {
			return em, 0, err
		}
		if name == "" || value == "" {
			return em, 0, formError(fat + " needs a name and a value")
		}
		em.Fields = append(em.Fields, chat.EmbedField{Name: name, Value: value, Inline: f.Inline})
		count(name)
		count(value)
	}
	return em, n, nil
}
