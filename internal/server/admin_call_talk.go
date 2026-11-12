package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/talks"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// hideCallTalks makes every administrative channel route (/channels/{id} and
// below: rename, delete, duplicate, auto-delete, overwrites, webhooks, any
// route added later) answer for a conversation's call Talk exactly what it
// answers for a channel that does not exist. A call Talk is as private as its
// conversation (doc/talks.md): the members who run it use PATCH /api/talks/{id},
// and no administrative act on it may reach the audit log, where the Owner
// would read who called whom. The id of the path is swapped for a fresh one
// before the route is chosen, so the handlers, their answers and the audit
// middleware cannot tell the two apart. A failed lookup refuses the request.
func hideCallTalks(q db.Querier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rctx := chi.RouteContext(r.Context())
			if rctx == nil {
				next.ServeHTTP(w, r)
				return
			}
			id, rest, ok := adminChannelPath(rctx.RoutePath)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			isCall, err := talks.IsDMTalk(r.Context(), q, id)
			if err != nil {
				httpx.WriteError(w, err)
				return
			}
			if isCall {
				rctx.RoutePath = "/channels/" + uuid.NewString() + rest
			}
			next.ServeHTTP(w, r)
		})
	}
}

// adminChannelPath splits "/channels/{uuid}[/rest]" into the id and the rest.
func adminChannelPath(path string) (id uuid.UUID, rest string, ok bool) {
	after, found := strings.CutPrefix(path, "/channels/")
	if !found {
		return uuid.Nil, "", false
	}
	raw, rest, _ := strings.Cut(after, "/")
	if rest != "" || strings.HasSuffix(after, "/") {
		rest = "/" + rest
	}
	id, err := uuid.Parse(raw)
	return id, rest, err == nil
}
