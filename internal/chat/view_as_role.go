package chat

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ViewAsChannel is a channel of the preview: what a member holding only
// @everyone and the previewed role would see and may do there.
type ViewAsChannel struct {
	ID     uuid.UUID   `json:"id" format:"uuid"`
	Number int64       `json:"number"`
	Name   string      `json:"name" maxLength:"64"`
	Type   ChannelType `json:"type"`
	// CanJoin is true for a Talk the previewed member may connect to; always
	// false for a text channel.
	CanJoin bool `json:"can_join"`
	// Permissions are the keys (GET /api/permissions) of the channel
	// permissions the previewed member holds here, in catalog order.
	Permissions []string `json:"permissions"`
}

// ViewAsCategory is a category of the preview with the channels the previewed
// member sees in it.
type ViewAsCategory struct {
	ID       uuid.UUID       `json:"id" format:"uuid"`
	Name     string          `json:"name" maxLength:"64"`
	Channels []ViewAsChannel `json:"channels"`
}

// ViewAsRole is the response of GET /api/admin/roles/{id}/view-as.
type ViewAsRole struct {
	RoleID   uuid.UUID `json:"role_id" format:"uuid"`
	RoleName string    `json:"role_name" maxLength:"32"`
	// Administrator is true when the role carries Administrator: such a member
	// sees everything the caller sees and ignores channel overwrites.
	Administrator bool             `json:"administrator"`
	Categories    []ViewAsCategory `json:"categories"`
	Uncategorized []ViewAsChannel  `json:"uncategorized"`
}

// viewAsRole handles GET /api/admin/roles/{id}/view-as.
//
// @Summary View the server as a role
// @Description A read-only preview of the sidebar for a made-up member who holds only @everyone and this role: the categories and channels that member can see, Talks marked can_join, and the channel permissions they hold in each channel, computed by the real permission engine (overwrites included). It never shows more than the caller can see themselves: a channel the caller cannot view is left out whatever the role could see. Needs Manage roles. Nobody is impersonated and nothing changes.
// @ID viewServerAsRole
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Param id path string true "Role ID (@everyone included)." Format(uuid)
// @Success 200 {object} ViewAsRole "Preview."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage roles permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/roles/{id}/view-as [get]
func (h *Handler) viewAsRole(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	view, sub, ok := snap.AsRoleOnly(id)
	if !ok {
		return httpx.ErrNotFound("role not found")
	}
	cats, uncat, err := GetServerHierarchy(r.Context(), h.DB)
	if err != nil {
		return err
	}
	// Start from what the caller sees: the preview is an intersection, so it
	// can never reveal a channel or category the caller has no access to.
	cats, uncat, err = h.visibleHierarchy(r, cats, uncat)
	if err != nil {
		return err
	}
	role, _ := snap.Role(id)
	p := &roleView{snap: view, sub: sub}
	out := ViewAsRole{RoleID: id, RoleName: role.Name, Administrator: role.Permissions.Has(perm.Administrator),
		Categories: []ViewAsCategory{}, Uncategorized: p.channels(uncat)}
	for _, c := range cats {
		chans := p.channels(c.Channels)
		if len(chans) > 0 || view.CategoryVisible(sub, c.ID) {
			out.Categories = append(out.Categories, ViewAsCategory{ID: c.ID, Name: c.Name, Channels: chans})
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// roleView answers permission questions for the previewed member.
type roleView struct {
	snap *perm.Snapshot
	sub  perm.Subject
}

// channels keeps the channels the previewed member can view.
func (v *roleView) channels(in []Channel) []ViewAsChannel {
	out := make([]ViewAsChannel, 0, len(in))
	for _, ch := range in {
		p := v.snap.Channel(v.sub, ch.ID)
		if !p.Has(perm.ViewChannel) {
			continue
		}
		out = append(out, ViewAsChannel{
			ID: ch.ID, Number: ch.Number, Name: ch.Name, Type: ch.Type,
			CanJoin:     ch.Type == ChannelTypeVoice && p.Has(perm.Connect),
			Permissions: channelPermissionKeys(p),
		})
	}
	return out
}

// channelPermissionKeys lists the channel-scoped permissions in p.
func channelPermissionKeys(p perm.Set) []string {
	keys := []string{}
	for _, info := range perm.Catalog {
		if info.Channel && p.Has(info.Bit) {
			keys = append(keys, info.Key)
		}
	}
	return keys
}
