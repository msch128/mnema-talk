package chat

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// likePattern escapes LIKE wildcards so user input matches literally.
func likePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}

// searchTerm is one word or "quoted phrase" of the text; Exclude is set for -term.
type searchTerm struct {
	Text    string
	Exclude bool
}

// parseSearchTerms splits the text into words, "quoted phrases" and -excluded
// words. A leading minus only excludes when something follows it; "-" alone,
// "a-b" and a quoted "-x" stay literal text.
func parseSearchTerms(text string) []searchTerm {
	var terms []searchTerm
	rs := []rune(text)
	for i := 0; i < len(rs); {
		if unicode.IsSpace(rs[i]) {
			i++
			continue
		}
		term, next := scanSearchTerm(rs, i)
		if term.Text != "" {
			terms = append(terms, term)
		}
		i = next
	}
	return terms
}

// scanSearchTerm reads the term that starts at rs[i] (not a space) and
// returns it with the index just after it.
func scanSearchTerm(rs []rune, i int) (searchTerm, int) {
	exclude := rs[i] == '-' && i+1 < len(rs) && !unicode.IsSpace(rs[i+1])
	if exclude {
		i++
	}
	if rs[i] == '"' {
		end := i + 1
		for end < len(rs) && rs[end] != '"' {
			end++
		}
		return searchTerm{Text: strings.TrimSpace(string(rs[i+1 : end])), Exclude: exclude}, min(end+1, len(rs))
	}
	end := i
	for end < len(rs) && !unicode.IsSpace(rs[end]) {
		end++
	}
	return searchTerm{Text: strings.TrimSpace(string(rs[i:end])), Exclude: exclude}, end
}

func isSearchEmpty(terms []searchTerm, q SearchQuery) bool {
	if hasIncluded(terms) || q.ChannelID != nil || q.AuthorID != nil || q.Mentions != nil {
		return false
	}
	return q.Has == "" && q.Is == "" && q.After == nil && q.BeforeDate == nil &&
		q.DuringStart == nil && q.Filename == "" && q.AuthorType == ""
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

var isoOffsetRe = regexp.MustCompile(`^([+-])(\d{1,2})(?::?(\d{2}))?$`)

func parseISOOffset(tz string) (*time.Location, bool) {
	m := isoOffsetRe.FindStringSubmatch(tz)
	if m == nil {
		return nil, false
	}
	sign := 1
	if m[1] == "-" {
		sign = -1
	}
	h, _ := strconv.Atoi(m[2])
	mins := 0
	if m[3] != "" {
		mins, _ = strconv.Atoi(m[3])
	}
	if h > 24 || mins > 59 {
		return nil, false
	}
	return time.FixedZone("", sign*(h*3600+mins*60)), true
}

func parseNumericOffset(tz string) (*time.Location, bool) {
	n, err := strconv.Atoi(tz)
	if err != nil {
		return nil, false
	}
	secs := n * 60
	if n < -14*60 || n > 14*60 {
		if n < -86400 || n > 86400 {
			return nil, false
		}
		secs = n
	}
	return time.FixedZone("", secs), true
}

// ambiguousOffsetRe matches a signed run of exactly three digits: "-300" is
// hh=3 mm=00 as an ISO offset but 300 minutes as a numeric one.
var ambiguousOffsetRe = regexp.MustCompile(`^[+-]\d{3}$`)

func parseTZLocation(tz string) (*time.Location, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" || strings.EqualFold(tz, "utc") || tz == "Z" {
		return time.UTC, nil
	}
	if ambiguousOffsetRe.MatchString(tz) {
		return nil, httpx.ErrInvalidInput("invalid tz offset: write +hh:mm or +hhmm")
	}
	if loc, ok := parseISOOffset(tz); ok {
		return loc, nil
	}
	if loc, ok := parseNumericOffset(tz); ok {
		return loc, nil
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return loc, nil
	}
	return nil, httpx.ErrInvalidInput("invalid tz offset")
}

// ParseSearchDay parses a YYYY-MM-DD date and a timezone offset, returning the day's UTC window.
func ParseSearchDay(during, tz string) (*time.Time, *time.Time, error) {
	if during == "" {
		return nil, nil, nil
	}
	t, err := time.Parse("2006-01-02", during)
	if err != nil {
		return nil, nil, httpx.ErrInvalidInput("during must be a YYYY-MM-DD date")
	}
	loc, err := parseTZLocation(tz)
	if err != nil {
		return nil, nil, err
	}
	y, m, d := t.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, loc).UTC()
	end := time.Date(y, m, d+1, 0, 0, 0, 0, loc).UTC()
	return &start, &end, nil
}

func hasIncluded(terms []searchTerm) bool {
	for _, t := range terms {
		if !t.Exclude {
			return true
		}
	}
	return false
}

func validateSearchTerms(q SearchQuery) ([]searchTerm, error) {
	if err := validateSearchDates(q); err != nil {
		return nil, err
	}
	switch q.Sort {
	case "", "newest", "oldest", sortRelevance:
	default:
		return nil, httpx.ErrInvalidInput("sort must be newest, oldest or relevance")
	}
	if utf8.RuneCountInString(q.Filename) > maxSearchQueryLen {
		return nil, httpx.ErrInvalidInput("filename is too long")
	}
	text := strings.TrimSpace(q.Text)
	if utf8.RuneCountInString(text) > maxSearchQueryLen {
		return nil, httpx.ErrInvalidInput("search text is too long")
	}
	terms := parseSearchTerms(text)
	if len(terms) > maxSearchTerms {
		terms = terms[:maxSearchTerms]
	}
	if isSearchEmpty(terms, q) {
		return nil, httpx.ErrInvalidInput("enter a search term or choose a filter")
	}
	return terms, nil
}

// sortRelevance is accepted for clients written against 1.1, which listed it
// next to newest and oldest. Search has no ranking, so it orders like newest.
const sortRelevance = "relevance"

// oldestFirst reports whether the results run oldest first; every other
// accepted sort is newest first. The order and the cursor comparison both
// follow from it.
func (q SearchQuery) oldestFirst() bool { return q.Sort == "oldest" }

type searchBuilder struct {
	where []string
	args  []any
}

func (b *searchBuilder) addArg(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *searchBuilder) addTerms(terms []searchTerm) {
	for _, t := range terms {
		not := ""
		if t.Exclude {
			not = "NOT "
		}
		b.where = append(b.where, "m.content "+not+"ILIKE "+b.addArg(likePattern(t.Text))+` ESCAPE '\'`)
	}
}

// addScope limits the search to what the caller may read: channels they can
// view, or the conversations they are in. The conversation rule is a
// membership test in SQL, whatever the permission state says, so a mistake
// elsewhere cannot make a conversation searchable by anybody else.
func (b *searchBuilder) addScope(q SearchQuery) {
	if q.Conversations {
		b.where = append(b.where, conversationReadableSQL(b.addArg(q.Member)))
		return
	}
	b.where = append(b.where, "NOT m.in_dm")
	if q.Visible != nil {
		b.where = append(b.where, "m.channel_id = ANY("+b.addArg(q.Visible)+"::uuid[])")
	}
}

func (b *searchBuilder) addAuthorFilters(q SearchQuery) error {
	if q.AuthorID != nil {
		b.where = append(b.where, "m.user_id = "+b.addArg(*q.AuthorID))
	}
	switch q.AuthorType {
	case "":
		return nil
	case "bot":
		b.where = append(b.where, "u.is_bot AND u.username NOT LIKE 'wh-%'")
	case "webhook":
		b.where = append(b.where, "u.is_bot AND u.username LIKE 'wh-%'")
	case "member":
		b.where = append(b.where, "NOT u.is_bot")
	default:
		return httpx.ErrInvalidInput("author must be bot, webhook or member")
	}
	return nil
}

func (b *searchBuilder) addContentFilters(q SearchQuery) error {
	if q.Filename != "" {
		b.where = append(b.where, mediaExists("f.original_filename ILIKE "+b.addArg(likePattern(q.Filename))+` ESCAPE '\'`))
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
	return nil
}

func (b *searchBuilder) addDateFilters(q SearchQuery) {
	if q.After != nil {
		b.where = append(b.where, "m.created_at > "+b.addArg(*q.After))
	}
	if q.BeforeDate != nil {
		b.where = append(b.where, "m.created_at < "+b.addArg(*q.BeforeDate))
	}
	if q.DuringStart != nil && q.DuringEnd != nil {
		b.where = append(b.where, "m.created_at >= "+b.addArg(*q.DuringStart)+" AND m.created_at < "+b.addArg(*q.DuringEnd))
	}
	if q.Before != nil {
		op := "<"
		if q.oldestFirst() {
			op = ">"
		}
		b.where = append(b.where, fmt.Sprintf("(m.created_at, m.id) %s (SELECT created_at, id FROM messages WHERE id = %s)", op, b.addArg(*q.Before)))
	}
}

func (b *searchBuilder) addFilters(q SearchQuery) error {
	b.addScope(q)
	if q.ChannelID != nil {
		b.where = append(b.where, "m.channel_id = "+b.addArg(*q.ChannelID))
	}
	if err := b.addAuthorFilters(q); err != nil {
		return err
	}
	if err := b.addContentFilters(q); err != nil {
		return err
	}
	b.addDateFilters(q)
	return nil
}

// mediaExists is the condition "the message has an attachment that is still
// there and satisfies cond" ("" for any): a deleted file is a placeholder, so
// no media filter matches it.
func mediaExists(cond string) string {
	if cond != "" {
		cond = " AND " + cond
	}
	return "EXISTS (SELECT 1 FROM media f WHERE f.message_id = m.id AND NOT f.is_deleted" + cond + ")"
}

// hasCondition is the WHERE condition of the "has" filter ("" for none).
func hasCondition(has string) (string, error) {
	switch has {
	case "":
		return "", nil
	case "file":
		return mediaExists(""), nil
	case "image":
		return mediaExists("f.mime_type LIKE 'image/%'"), nil
	case "video":
		return mediaExists("f.mime_type LIKE 'video/%'"), nil
	case "link":
		return `m.content ~* 'https?://'`, nil
	case "embed":
		return "jsonb_array_length(m.embeds) > 0", nil
	case "poll":
		return "EXISTS (SELECT 1 FROM polls p WHERE p.message_id = m.id)", nil
	case "edit":
		return "m.is_edited", nil
	}
	return "", httpx.ErrInvalidInput("has must be file, image, video, link, embed, poll or edit")
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
