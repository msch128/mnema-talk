package audit

import (
	"bytes"
	"cmp"
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
		if _, err := c.buf.Write(p[:min(room, len(p))]); err != nil {
			return 0, err
		}
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

type routeMatch struct {
	rt     Route
	params map[string]string
	known  bool
}

func matchAuditableRoute(r *http.Request) (routeMatch, bool) {
	if !mutating(r.Method) {
		return routeMatch{}, false
	}
	rt, params, known := Match(r.Method, r.URL.Path)
	if known && rt.Skip {
		return routeMatch{}, false
	}
	return routeMatch{rt: rt, params: params, known: known}, true
}

func (l *Log) captureBefore(ctx context.Context, rt Route, params map[string]string, known bool) (map[string]any, string) {
	if !known || rt.Snapshot == ResNone {
		return nil, ""
	}
	return Snapshot(ctx, l.DB, rt.Snapshot, params[rt.IDParam])
}

func isSuccessStatus(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func attachChannelID(e *Entry) {
	if e.TargetType != "channel" {
		return
	}
	id, err := uuid.Parse(e.TargetID)
	if err != nil {
		return
	}
	e.ChannelID = &id
}

func (l *Log) finalizeEntry(
	ctx context.Context,
	r *http.Request,
	m routeMatch,
	before map[string]any,
	name string,
	req map[string]any,
	reqMark string,
	response []byte,
) Entry {
	e := newEntry(r, m.rt, m.params, m.known)
	e.TargetName = name
	addRequestDetails(&e, m.params, req, reqMark)
	if before != nil {
		l.addStateChange(ctx, &e, m.rt, m.params, before)
	}
	if e.TargetID == "" {
		takeCreatedTarget(&e, response)
		l.addCreatedState(ctx, &e, m)
	}
	attachChannelID(&e)
	return e
}

// addCreatedState records the audited fields of a resource the request
// created, once its id is known from the response.
func (l *Log) addCreatedState(ctx context.Context, e *Entry, m routeMatch) {
	if !m.known || m.rt.Snapshot == ResNone || m.rt.IDParam != "" || e.TargetID == "" {
		return
	}
	if after, _ := Snapshot(ctx, l.DB, m.rt.Snapshot, e.TargetID); after != nil {
		e.Details["after"] = after
	}
}

// serve runs the request and records it if it was a successful change.
func (l *Log) serve(next http.Handler, w http.ResponseWriter, r *http.Request) {
	m, auditable := matchAuditableRoute(r)
	if !auditable {
		next.ServeHTTP(w, r)
		return
	}
	ctx := r.Context()
	req, reqMark := peekRequest(r, m.known && m.rt.NoBody)
	before, name := l.captureBefore(ctx, m.rt, m.params, m.known)

	ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
	resp := &capped{max: maxResponseSnip}
	ww.Tee(resp)
	next.ServeHTTP(ww, r)
	if !isSuccessStatus(ww.Status()) {
		return
	}

	e := l.finalizeEntry(ctx, r, m, before, name, req, reqMark, resp.buf.Bytes())
	l.RecordOrLog(ctx, e)
}

func shouldSkipBody(r *http.Request, noBody bool) bool {
	if noBody || r.Body == nil {
		return true
	}
	if r.Body == http.NoBody {
		return true
	}
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/")
}

// peekRequest reads the body for the entry and puts it back for the handler.
// It is read whatever the Content-Type says: the handler may decode it
// regardless, and an entry must not lose its request to a mislabelled one.
// Anything that is not a JSON object, or does not fit in the limit, is marked
// instead of dropped silently.
func peekRequest(r *http.Request, noBody bool) (req map[string]any, mark string) {
	if shouldSkipBody(r, noBody) {
		return nil, ""
	}
	head, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	// Whatever was read goes back in front of the rest, even after a read
	// error, so the handler sees the same bytes (and the same error) as without us.
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), r.Body), r.Body}
	if err != nil {
		return nil, "request_unparsed"
	}
	switch {
	case len(head) > maxRequestBody:
		mark = "request_truncated"
	case len(bytes.TrimSpace(head)) == 0:
	default:
		if err := json.Unmarshal(head, &req); err != nil {
			req, mark = nil, "request_unparsed"
		}
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
	after, name := Snapshot(ctx, l.DB, rt.Snapshot, params[rt.IDParam])
	if after == nil {
		return
	}
	if e.TargetName == "" && name != "" {
		e.TargetName = name
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
	if err := json.Unmarshal(response, &out); err != nil {
		return
	}
	// Never blank what an earlier step (the snapshot) already named.
	if out.ID != "" {
		e.TargetID = out.ID
	}
	if e.TargetName == "" {
		e.TargetName = cmp.Or(out.Name, out.Username)
	}
}
