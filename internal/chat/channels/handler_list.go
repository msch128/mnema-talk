package channels

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// ListChannels handles GET /api/channels.
//
// @Summary List categories and channels
// @Description Only the channels the caller may view; a category shows when its own permissions allow it or any of its channels is visible.
// @ID listChannels
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Success 200 {object} channels.ChannelHierarchy "Channel hierarchy."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels [get]
func (a *API) ListChannels(w http.ResponseWriter, r *http.Request) error {
	cats, uncat, err := GetServerHierarchy(r.Context(), a.DB)
	if err != nil {
		return err
	}
	cats, uncat, err = VisibleHierarchy(a.Guard.Perms, r, cats, uncat)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, ChannelHierarchy{Categories: cats, Uncategorized: uncat})
	return nil
}
