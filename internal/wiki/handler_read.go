package wiki

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// TreeResponse is the answer of GET /api/wiki.
type TreeResponse struct {
	Pages []PageSummary `json:"pages"`
}

// list handles GET /api/wiki.
//
// @Summary List the wiki pages
// @Description The pages without their text, in tree order. With deleted=true, holders of Manage wiki also get the deleted pages (they carry deleted_at). Needs View wiki.
// @ID listWikiPages
// @Tags Wiki
// @Produce json
// @Security cookieAuth
// @Param deleted query bool false "Include deleted pages (Manage wiki)."
// @Success 200 {object} TreeResponse "The pages."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing View wiki, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	if err := h.require(r, perm.ViewWiki); err != nil {
		return err
	}
	deleted := r.URL.Query().Get("deleted") == "true" && h.can(r, perm.ManageWiki)
	pages, err := h.Store.Tree(r.Context(), deleted)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, TreeResponse{Pages: pages})
	return nil
}

// get handles GET /api/wiki/pages/{slug}.
//
// @Summary Get a wiki page
// @Description The page with its text. A renamed page is found at its old address too (redirected_from names it). A deleted page is only found by holders of Manage wiki. Needs View wiki.
// @ID getWikiPage
// @Tags Wiki
// @Produce json
// @Security cookieAuth
// @Param slug path string true "The page's address."
// @Success 200 {object} Page "The page."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing View wiki, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such page."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages/{slug} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	if err := h.require(r, perm.ViewWiki); err != nil {
		return err
	}
	p, err := h.Store.Get(r.Context(), chi.URLParam(r, "slug"), h.can(r, perm.ManageWiki))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

// revisions handles GET /api/wiki/pages/{slug}/revisions.
//
// @Summary List the revisions of a wiki page
// @Description The saved states of the page, newest first, without their text: who saved, when, the summary, the size, and the revision a restore copied. Page with before (the lowest revision of the previous page). Needs View wiki.
// @ID listWikiRevisions
// @Tags Wiki
// @Produce json
// @Security cookieAuth
// @Param slug path string true "The page's address."
// @Param before query int false "Only revisions below this number."
// @Success 200 {array} Revision "The revisions."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing View wiki, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such page."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages/{slug}/revisions [get]
func (h *Handler) revisions(w http.ResponseWriter, r *http.Request) error {
	if err := h.require(r, perm.ViewWiki); err != nil {
		return err
	}
	before := httpx.QueryLimit(r, "before", 0, 1<<30)
	list, err := h.Store.Revisions(r.Context(), chi.URLParam(r, "slug"), before, RevisionPage, h.can(r, perm.ManageWiki))
	if err != nil {
		return err
	}
	if list == nil {
		list = []Revision{}
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// revision handles GET /api/wiki/pages/{slug}/revisions/{rev}.
//
// @Summary Get one revision of a wiki page
// @Description The title and text of a saved state of the page. Needs View wiki.
// @ID getWikiRevision
// @Tags Wiki
// @Produce json
// @Security cookieAuth
// @Param slug path string true "The page's address."
// @Param rev path int true "The revision number."
// @Success 200 {object} Revision "The revision."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): bad revision number."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing View wiki, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such page or revision (old ones are pruned)."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages/{slug}/revisions/{rev} [get]
func (h *Handler) revision(w http.ResponseWriter, r *http.Request) error {
	if err := h.require(r, perm.ViewWiki); err != nil {
		return err
	}
	rev, err := pathRev(r)
	if err != nil {
		return err
	}
	out, err := h.Store.Revision(r.Context(), chi.URLParam(r, "slug"), rev, h.can(r, perm.ManageWiki))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// search handles GET /api/wiki/search.
//
// @Summary Search the wiki
// @Description Pages whose title or text contains q (at least three characters), titles first, at most 30, with a few words around the match. Deleted pages are never found. Needs View wiki.
// @ID searchWiki
// @Tags Wiki
// @Produce json
// @Security cookieAuth
// @Param q query string true "What to look for."
// @Success 200 {array} SearchHit "The matching pages."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing View wiki, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/search [get]
func (h *Handler) search(w http.ResponseWriter, r *http.Request) error {
	if err := h.require(r, perm.ViewWiki); err != nil {
		return err
	}
	hits, err := h.Store.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, hits)
	return nil
}
