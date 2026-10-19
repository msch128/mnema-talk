package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

const (
	maxRequestBody  = 64 << 10
	maxResponseSnip = 16 << 10
)

// capped keeps the first max bytes written to it.
type capped struct {
	buf bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// Middleware records every successful mutating request below /api/admin. It
// must sit inside the admin group, after the permission check: refused
// requests changed nothing and leave no entry. Failures to write the entry
// are logged, not returned, because the change already happened.
func (l *Log) Middleware(next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.serve(next, w, r)
	})
}

// serve runs the request and records it if it was a successful change.
func (l *Log) serve(next http.Handler, w http.ResponseWriter, r *http.Request) {
	if !mutating(r.Method) {
		next.ServeHTTP(w, r)
		return
	}
	rt, params, known := Match(r.Method, r.URL.Path)
	if known && rt.Skip {
		next.ServeHTTP(w, r)
		return
	}
	ctx := r.Context()
	req, reqMark := peekRequest(r, known && rt.NoBody)

	var before map[string]any
	var name string
	if known && rt.Snapshot != ResNone {
		before, name = Snapshot(ctx, l.DB, rt.Snapshot, params[rt.IDParam])
	}

	ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
	resp := &capped{max: maxResponseSnip}
	ww.Tee(resp)
	next.ServeHTTP(ww, r)
	if st := ww.Status(); st < 200 || st >= 300 {
		return
	}

	e := newEntry(r, rt, params, known)
	e.TargetName = name
	addRequestDetails(&e, params, req, reqMark)
	if before != nil {
		l.addStateChange(ctx, &e, rt, params, before)
	}
	if e.TargetID == "" {
		takeCreatedTarget(&e, resp.buf.Bytes())
	}
	if e.TargetType == "channel" {
		if id, err := uuid.Parse(e.TargetID); err == nil {
			e.ChannelID = &id
		}
	}
	l.RecordOrLog(ctx, e)
}

// peekRequest reads the body for the entry and puts it back for the handler.
// It is read whatever the Content-Type says: the handler may decode it
// regardless, and an entry must not lose its request to a mislabelled one.
// Anything that is not a JSON object, or does not fit in the limit, is marked
// instead of dropped silently.
func peekRequest(r *http.Request, noBody bool) (req map[string]any, mark string) {
	if noBody || r.Body == nil || r.Body == http.NoBody || strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		return nil, ""
	}
	head, _ := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), r.Body), r.Body}
	switch {
	case len(head) > maxRequestBody:
		mark = "request_truncated"
	case len(bytes.TrimSpace(head)) == 0:
	case json.Unmarshal(head, &req) != nil:
		req, mark = nil, "request_unparsed"
	}
	return req, mark
}

// newEntry names the action and target of the request. A route the table does
// not know is logged by method and path.
func newEntry(r *http.Request, rt Route, params map[string]string, known bool) Entry {
	e := Entry{Details: map[string]any{}}
	if known {
		e.Action, e.TargetType = rt.Action, rt.TargetType
		e.TargetID = params[rt.IDParam]
		return e
	}
	e.Action = "admin." + strings.ToLower(r.Method)
	e.Details["path"] = strings.TrimPrefix(r.URL.Path, AdminPrefix)
	return e
}

// addRequestDetails keeps the path parameters and the request body (or the
// mark saying why it is missing) with the entry.
func addRequestDetails(e *Entry, params map[string]string, req map[string]any, reqMark string) {
	if len(params) > 0 {
		e.Details["params"] = params
	}
	if reqMark != "" {
		e.Details[reqMark] = true
	}
	if req != nil {
		e.Details["request"] = req
	}
}

// addStateChange records the state before and after the handler ran. It
// shows what the handler really did, not what the request claims. A deleted
// resource has no state afterwards.
func (l *Log) addStateChange(ctx context.Context, e *Entry, rt Route, params map[string]string, before map[string]any) {
	e.Details["before"] = before
	after, _ := Snapshot(ctx, l.DB, rt.Snapshot, params[rt.IDParam])
	if after == nil {
		return
	}
	e.Details["after"] = after
	if ch := diff(before, after); len(ch) > 0 {
		e.Details["changes"] = ch
	}
}

// takeCreatedTarget takes the new id and name of a creation from the
// response, and nothing else (it may carry a token or an invite code).
func takeCreatedTarget(e *Entry, response []byte) {
	var out struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Username string `json:"username"`
	}
	if json.Unmarshal(response, &out) == nil {
		e.TargetID, e.TargetName = out.ID, out.Name
	}
}
