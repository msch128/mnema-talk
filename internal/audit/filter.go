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

// restrictTo hides what the reader may not see: class VisPrivileged is hidden;
// VisChannel only for channels the reader may view right now.
func (f *filter) restrictTo(viewer Viewer) {
	if viewer.Privileged {
		return
	}
	f.add("action NOT LIKE ALL(?)", patterns(VisPrivileged))
	f.args = append(f.args, patterns(VisChannel), viewer.Channels)
	f.where = append(f.where, "(action NOT LIKE ALL($"+strconv.Itoa(len(f.args)-1)+") OR channel_id = ANY($"+strconv.Itoa(len(f.args))+"))")
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
