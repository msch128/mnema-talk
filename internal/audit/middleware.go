package audit

import (
	"bytes"
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

		// The body is read whatever the Content-Type says: the handler may
		// decode it regardless, and an entry must not lose its request to a
		// mislabelled one. Anything that is not a JSON object, or does not fit
		// in the limit, is marked instead of dropped silently.
		var req map[string]any
		var reqMark string
		if !(known && rt.NoBody) && r.Body != nil && r.Body != http.NoBody && !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			head, _ := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
			r.Body = struct {
				io.Reader
				io.Closer
			}{io.MultiReader(bytes.NewReader(head), r.Body), r.Body}
			switch {
			case len(head) > maxRequestBody:
				reqMark = "request_truncated"
			case len(bytes.TrimSpace(head)) == 0:
			case json.Unmarshal(head, &req) != nil:
				req, reqMark = nil, "request_unparsed"
			}
		}

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

		e := Entry{Details: map[string]any{}}
		if known {
			e.Action, e.TargetType = rt.Action, rt.TargetType
			e.TargetID = params[rt.IDParam]
		} else {
			e.Action = "admin." + strings.ToLower(r.Method)
			e.Details["path"] = strings.TrimPrefix(r.URL.Path, AdminPrefix)
		}
		e.TargetName = name
		if len(params) > 0 {
			e.Details["params"] = params
		}
		if reqMark != "" {
			e.Details[reqMark] = true
		}
		if req != nil {
			e.Details["request"] = req
		}
		if before != nil {
			e.Details["before"] = before
			// What the handler really did: compare with the state afterwards,
			// not with what the request claims. A deleted resource has none.
			if after, _ := Snapshot(ctx, l.DB, rt.Snapshot, params[rt.IDParam]); after != nil {
				e.Details["after"] = after
				if ch := diff(before, after); len(ch) > 0 {
					e.Details["changes"] = ch
				}
			}
		}
		if e.TargetID == "" {
			// A creation: take the new id and name from the response, and
			// nothing else (it may carry a token or an invite code).
			var out struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Username string `json:"username"`
			}
			if json.Unmarshal(resp.buf.Bytes(), &out) == nil {
				e.TargetID, e.TargetName = out.ID, out.Name
			}
		}
		if e.TargetType == "channel" {
			if id, err := uuid.Parse(e.TargetID); err == nil {
				e.ChannelID = &id
			}
		}
		l.RecordOrLog(ctx, e)
	})
}
