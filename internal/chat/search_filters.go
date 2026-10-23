package chat

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// likePattern escapes LIKE wildcards so user input matches literally.
func likePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}

func isSearchEmpty(terms []string, q SearchQuery) bool {
	if len(terms) > 0 || q.ChannelID != nil || q.AuthorID != nil || q.Mentions != nil {
		return false
	}
	return q.Has == "" && q.Is == "" && q.After == nil && q.BeforeDate == nil
}

// validateSearchDates rejects an empty time window (After not before BeforeDate).
func validateSearchDates(q SearchQuery) error {
	if q.After != nil && q.BeforeDate != nil && !q.After.Before(*q.BeforeDate) {
		return httpx.ErrInvalidInput("after must be earlier than before_date")
	}
	return nil
}

// ParseSearchTime parses an RFC 3339 timestamp query parameter.
func ParseSearchTime(name, v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, httpx.ErrInvalidInput(name + " must be an RFC 3339 timestamp")
	}
	return &t, nil
}

func validateSearchTerms(q SearchQuery) ([]string, error) {
	if err := validateSearchDates(q); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(q.Text)
	if utf8.RuneCountInString(text) > maxSearchQueryLen {
		return nil, httpx.ErrInvalidInput("search text is too long")
	}
	terms := strings.Fields(text)
	if len(terms) > maxSearchTerms {
		terms = terms[:maxSearchTerms]
	}
	if isSearchEmpty(terms, q) {
		return nil, httpx.ErrInvalidInput("enter a search term or choose a filter")
	}
	return terms, nil
}

type searchBuilder struct {
	where []string
	args  []any
}

func (b *searchBuilder) addArg(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *searchBuilder) addTerms(terms []string) {
	for _, t := range terms {
		b.where = append(b.where, "m.content ILIKE "+b.addArg(likePattern(t))+` ESCAPE '\'`)
	}
}

// addScope limits the search to what the caller may read: channels they can
// view, or the conversations they are in. The conversation rule is a
// membership test in SQL, whatever the permission state says, so a mistake
// elsewhere cannot make a conversation searchable by anybody else.
func (b *searchBuilder) addScope(q SearchQuery) {
	if q.Conversations {
		b.where = append(b.where, "m.in_dm AND EXISTS (SELECT 1 FROM dm_members dm WHERE dm.channel_id = m.channel_id AND dm.user_id = "+b.addArg(q.Member)+")")
		return
	}
	b.where = append(b.where, "NOT m.in_dm")
	if q.Visible != nil {
		b.where = append(b.where, "m.channel_id = ANY("+b.addArg(q.Visible)+"::uuid[])")
	}
}

func (b *searchBuilder) addFilters(q SearchQuery) error {
	b.addScope(q)
	if q.ChannelID != nil {
		b.where = append(b.where, "m.channel_id = "+b.addArg(*q.ChannelID))
	}
	if q.AuthorID != nil {
		b.where = append(b.where, "m.user_id = "+b.addArg(*q.AuthorID))
	}
	has, err := hasCondition(q.Has)
	if err != nil {
		return err
	}
	if has != "" {
		b.where = append(b.where, has)
	}
	is, err := isCondition(q.Is)
	if err != nil {
		return err
	}
	if is != "" {
		b.where = append(b.where, is)
	}
	if q.Mentions != nil {
		b.where = append(b.where, "EXISTS (SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.user_id = "+b.addArg(*q.Mentions)+")")
	}
	if q.After != nil {
		b.where = append(b.where, "m.created_at > "+b.addArg(*q.After))
	}
	if q.BeforeDate != nil {
		b.where = append(b.where, "m.created_at < "+b.addArg(*q.BeforeDate))
	}
	if q.Before != nil {
		b.where = append(b.where, "(m.created_at, m.id) < (SELECT created_at, id FROM messages WHERE id = "+b.addArg(*q.Before)+")")
	}
	return nil
}

// hasCondition is the WHERE condition of the "has" filter ("" for none).
func hasCondition(has string) (string, error) {
	switch has {
	case "":
		return "", nil
	case "file":
		return "EXISTS (SELECT 1 FROM media f WHERE f.message_id = m.id)", nil
	case "image":
		return "EXISTS (SELECT 1 FROM media f WHERE f.message_id = m.id AND f.mime_type LIKE 'image/%')", nil
	case "video":
		return "EXISTS (SELECT 1 FROM media f WHERE f.message_id = m.id AND f.mime_type LIKE 'video/%')", nil
	case "link":
		return `m.content ~* 'https?://'`, nil
	case "poll":
		return "EXISTS (SELECT 1 FROM polls p WHERE p.message_id = m.id)", nil
	case "edit":
		return "m.is_edited", nil
	}
	return "", httpx.ErrInvalidInput("has must be file, image, video, link, poll or edit")
}

// isCondition is the WHERE condition of the "is" filter ("" for none).
func isCondition(is string) (string, error) {
	switch is {
	case "":
		return "", nil
	case "pinned":
		return "m.is_pinned", nil
	}
	return "", httpx.ErrInvalidInput("is must be pinned")
}
