package audit

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// filter builds the WHERE clause of an audit log listing; every value is
// passed as a numbered query argument.
type filter struct {
	where []string
	args  []any
}

// add appends a condition whose "?" stands for v.
func (f *filter) add(cond string, v any) {
	f.args = append(f.args, v)
	f.where = append(f.where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(f.args))))
}

// parse turns the query parameters of the listing into conditions.
func (f *filter) parse(q url.Values) error {
	if err := f.parseSubject(q); err != nil {
		return err
	}
	return f.parseRange(q)
}

// parseSubject handles who did what to what: actor, action, target_type, target_id.
func (f *filter) parseSubject(q url.Values) error {
	if v := q.Get("actor"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.ErrInvalidInput("invalid actor")
		}
		f.add("actor_id = ?", id)
	}
	if v := q.Get("action"); v != "" {
		if len(v) > 64 {
			return httpx.ErrInvalidInput("invalid action")
		}
		f.args = append(f.args, v, v+".%")
		f.where = append(f.where, "(action = $"+strconv.Itoa(len(f.args)-1)+" OR action LIKE $"+strconv.Itoa(len(f.args))+")")
	}
	if v := q.Get("target_type"); v != "" {
		f.add("target_type = ?", v)
	}
	if v := q.Get("target_id"); v != "" {
		f.add("target_id = ?", v)
	}
	return nil
}

// parseRange handles when: from, to and the paging cursor before.
func (f *filter) parseRange(q url.Values) error {
	if v := q.Get("from"); v != "" {
		t, err := parseTime(v, false)
		if err != nil {
			return httpx.ErrInvalidInput("invalid from")
		}
		f.add("created_at >= ?", t)
	}
	if v := q.Get("to"); v != "" {
		t, err := parseTime(v, true)
		if err != nil {
			return httpx.ErrInvalidInput("invalid to")
		}
		f.add("created_at < ?", t)
	}
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			return httpx.ErrInvalidInput("invalid before")
		}
		f.add("id < ?", n)
	}
	return nil
}

// restrictTo hides what the reader may not see. An entry gets the class of the
// first row of Classes its action matches, the same rule as ClassOf: class
// VisPrivileged is hidden, VisChannel shown only for channels the reader may
// view right now (an entry without a channel is hidden), VisAuditors shown.
// The catch-all row is VisPrivileged, so an action without a row is hidden.
// One kind of stored row is classed before the table: see legacyTalkRemoval.
func (f *filter) restrictTo(viewer Viewer) {
	if viewer.Privileged {
		return
	}
	f.args = append(f.args, viewer.Channels)
	channels := "$" + strconv.Itoa(len(f.args))
	var b strings.Builder
	b.WriteString("CASE" + legacyTalkRemoval(channels))
	for _, c := range Classes {
		b.WriteString(" WHEN " + f.matches(c.Pattern) + " THEN " + shownSQL(c.Vis, channels))
	}
	b.WriteString(" ELSE FALSE END")
	f.where = append(f.where, b.String())
}

// legacyTalkRemoval classes the rows the per-Talk removal wrote before it
// became voice.disconnect (H-L6-03): user.talk_kick with the Talk in
// details.channel_id and no channel_id column. The admin-wide kick, which
// keeps user.talk_kick, never has that key at the top of its details (they
// hold path, params, request, before, after and changes), so such a row is a
// per-Talk removal and is shown like voice.disconnect: only to readers who can
// view that Talk. audit_log is immutable, so the rows are classed when read.
// The id is compared as a uuid; one that is not a uuid is never shown, and the
// inner CASE keeps the cast from failing the whole listing.
func legacyTalkRemoval(channels string) string {
	const id = "details->>'channel_id'"
	return " WHEN action = 'user.talk_kick' AND " + id + " IS NOT NULL THEN COALESCE(CASE WHEN " + id +
		" ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'" +
		" THEN CAST(" + id + " AS uuid) = ANY(" + channels + ") END, FALSE)"
}

// matches is the SQL of matchLike: a literal, or a literal prefix and %.
func (f *filter) matches(pattern string) string {
	if pattern == "%" {
		return "TRUE"
	}
	prefix, isPrefix := strings.CutSuffix(pattern, "%")
	f.args = append(f.args, prefix)
	arg := "$" + strconv.Itoa(len(f.args))
	if isPrefix {
		return "starts_with(action, " + arg + ")"
	}
	return "action = " + arg
}

// shownSQL says whether a reader without privileges sees an entry of class v.
func shownSQL(v Visibility, channels string) string {
	switch v {
	case VisAuditors:
		return "TRUE"
	case VisChannel:
		return "COALESCE(channel_id = ANY(" + channels + "), FALSE)"
	}
	return "FALSE"
}

// query returns the SELECT for the newest entries matching the filter.
func (f filter) query(limit int) (string, []any) {
	sql := `SELECT id, created_at, actor_id, actor_name, action, target_type, target_id, target_name, details, request_id FROM audit_log`
	if len(f.where) > 0 {
		sql += " WHERE " + strings.Join(f.where, " AND ")
	}
	args := append(f.args[:len(f.args):len(f.args)], limit)
	sql += " ORDER BY id DESC LIMIT $" + strconv.Itoa(len(args))
	return sql, args
}
