package wiki

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// create handles POST /api/wiki/pages.
//
// @Summary Create a wiki page
// @Description Adds a page below a parent page (none: at the top) as its first revision. The address derives from the title when it is not given. At most 500 pages, 5 levels, 50 KB of text. Needs Edit wiki, which a timeout and the new-member wait take away. Counts against the member's wiki write limit (429).
// @ID createWikiPage
// @Tags Wiki
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body PageInput true "Request body."
// @Success 201 {object} Page "The new page."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad address, text too long or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing Edit wiki, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the parent page does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the address is taken, or the page limit or depth is reached."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	var in PageInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.require(r, perm.EditWiki); err != nil {
		return err
	}
	p, err := h.Store.Create(r.Context(), h.userID(r), in)
	if err != nil {
		return err
	}
	h.record(r, "wiki.create", p, map[string]any{"slug": p.Slug})
	httpx.WriteJSON(w, http.StatusCreated, p)
	return nil
}

// update handles PATCH /api/wiki/pages/{slug}.
//
// @Summary Change a wiki page
// @Description A change of the title or text needs Edit wiki, base_rev (the revision the editor saw) and an unlocked page, and writes a new revision; when the page has moved on meanwhile it answers 409 and changes nothing. A change of the address (the old one redirects), the parent or the position needs Manage wiki and writes no revision. Holders of Manage wiki may change a locked page. A change that leaves the page as it is answers 200 and writes nothing.
// @ID updateWikiPage
// @Tags Wiki
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param slug path string true "The page's address."
// @Param request body PageChange true "Request body."
// @Success 200 {object} Page "The page after the change."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, missing base_rev, text too long or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing Edit wiki or Manage wiki, the page is locked, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such page or parent."
// @Failure 409 {object} httpx.ErrorResponse "STALE_REVISION: the page changed meanwhile. CONFLICT: the address is taken, or the move passes the depth limit or goes under the page itself."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages/{slug} [patch]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	var in PageChange
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if in.Title != nil || in.Content != nil {
		if err := h.require(r, perm.EditWiki); err != nil {
			return err
		}
	}
	if in.Slug != nil || in.ParentID != nil || in.Position != nil {
		if err := h.require(r, perm.ManageWiki); err != nil {
			return err
		}
	}
	done, err := h.Store.Change(r.Context(), h.userID(r), chi.URLParam(r, "slug"), in, h.can(r, perm.ManageWiki))
	if err != nil {
		return err
	}
	if done.Revised {
		h.record(r, "wiki.edit", done.Page, map[string]any{"rev": done.Page.Rev, "summary": in.Summary})
	}
	if done.Renamed || done.Moved {
		h.record(r, "wiki.move", done.Page, map[string]any{"slug": done.Page.Slug, "from_slug": done.OldSlug})
	}
	httpx.WriteJSON(w, http.StatusOK, done.Page)
	return nil
}

// remove handles DELETE /api/wiki/pages/{slug}.
//
// @Summary Delete a wiki page
// @Description Takes the page out of the wiki. Nothing is lost: it keeps its address and revisions, and holders of Manage wiki can restore it. A page with subpages cannot be deleted. Needs Manage wiki.
// @ID deleteWikiPage
// @Tags Wiki
// @Produce json
// @Security cookieAuth
// @Param slug path string true "The page's address."
// @Success 200 {object} Page "The deleted page."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing Manage wiki, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such page."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the page has subpages."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages/{slug} [delete]
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) error {
	if err := h.require(r, perm.ManageWiki); err != nil {
		return err
	}
	p, err := h.Store.Delete(r.Context(), h.userID(r), chi.URLParam(r, "slug"))
	if err != nil {
		return err
	}
	h.record(r, "wiki.delete", p, map[string]any{"slug": p.Slug})
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

// undelete handles POST /api/wiki/pages/{slug}/undelete.
//
// @Summary Restore a deleted wiki page
// @Description Puts a deleted page back below its old parent (at the top when that one is gone). Needs Manage wiki.
// @ID undeleteWikiPage
// @Tags Wiki
// @Produce json
// @Security cookieAuth
// @Param slug path string true "The page's address."
// @Success 200 {object} Page "The restored page."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing Manage wiki, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such page."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the page is not deleted, or the page limit is reached."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages/{slug}/undelete [post]
func (h *Handler) undelete(w http.ResponseWriter, r *http.Request) error {
	if err := h.require(r, perm.ManageWiki); err != nil {
		return err
	}
	p, err := h.Store.Undelete(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		return err
	}
	h.record(r, "wiki.restore", p, map[string]any{"kind": "page", "slug": p.Slug})
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

// lock handles PUT /api/wiki/pages/{slug}/lock.
//
// @Summary Lock or unlock a wiki page
// @Description While a page is locked only holders of Manage wiki change its title or text. Needs Manage wiki.
// @ID lockWikiPage
// @Tags Wiki
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param slug path string true "The page's address."
// @Param request body LockInput true "Request body."
// @Success 200 {object} Page "The page."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON or unknown JSON fields."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing Manage wiki, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such page."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages/{slug}/lock [put]
func (h *Handler) lock(w http.ResponseWriter, r *http.Request) error {
	var in LockInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.require(r, perm.ManageWiki); err != nil {
		return err
	}
	p, err := h.Store.SetLocked(r.Context(), chi.URLParam(r, "slug"), in.Locked)
	if err != nil {
		return err
	}
	h.record(r, "wiki.lock", p, map[string]any{"locked": in.Locked})
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

// restore handles POST /api/wiki/pages/{slug}/revisions/{rev}/restore.
//
// @Summary Restore a revision of a wiki page
// @Description Makes the title and text of a revision the page's again, as a new revision (nothing is overwritten or removed), so vandalism is undone in one step. base_rev is the latest revision the editor saw; 409 when the page has moved on. Needs Edit wiki and an unlocked page (or Manage wiki).
// @ID restoreWikiRevision
// @Tags Wiki
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param slug path string true "The page's address."
// @Param rev path int true "The revision to restore."
// @Param request body RestoreInput true "Request body."
// @Success 200 {object} Page "The page after the restore."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, missing base_rev, bad revision number, or the page already is that revision."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing Edit wiki, the page is locked, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such page or revision."
// @Failure 409 {object} httpx.ErrorResponse "STALE_REVISION: the page changed meanwhile."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/wiki/pages/{slug}/revisions/{rev}/restore [post]
func (h *Handler) restore(w http.ResponseWriter, r *http.Request) error {
	rev, err := pathRev(r)
	if err != nil {
		return err
	}
	var in RestoreInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := h.require(r, perm.EditWiki); err != nil {
		return err
	}
	p, err := h.Store.RestoreRevision(r.Context(), h.userID(r), chi.URLParam(r, "slug"), rev, in, h.can(r, perm.ManageWiki))
	if err != nil {
		return err
	}
	h.record(r, "wiki.restore", p, map[string]any{"kind": "revision", "restored_from": rev, "rev": p.Rev})
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}
