package roles

import (
	"bytes"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ChannelPermissions is what the caller may do in one channel.
type ChannelPermissions struct {
	ChannelID   uuid.UUID `json:"channel_id" format:"uuid"`
	Permissions perm.Set  `json:"permissions" swaggertype:"integer" minimum:"0"`
}

// NewMemberInfo tells a new member when each action opens up. A missing time
// means that action is not held back.
type NewMemberInfo struct {
	WriteAt *time.Time `json:"write_at,omitempty" binding:"optional" format:"date-time"`
	ReactAt *time.Time `json:"react_at,omitempty" binding:"optional" format:"date-time"`
	SpeakAt *time.Time `json:"speak_at,omitempty" binding:"optional" format:"date-time"`
}

// PermissionsInfo is what the caller may do.
type PermissionsInfo struct {
	// Owner is true for the Owner, who may do everything and cannot be
	// restricted.
	Owner bool `json:"owner"`
	// Permissions are the server-wide permissions (bits, see catalog).
	Permissions perm.Set `json:"permissions" swaggertype:"integer" minimum:"0"`
	// RoleIDs are the caller's custom roles, highest first.
	RoleIDs []uuid.UUID `json:"role_ids" format:"uuid"`
	// TopPosition is the position of the caller's highest role (0 without
	// one); the Owner is above every position. A member can only manage roles
	// and members below it.
	TopPosition int `json:"top_position" minimum:"0"`
	// RulesPending is set while the caller has to accept the community rules
	// (POST /api/onboarding/accept); posting, reacting, uploading and joining
	// a Talk answer RULES_NOT_ACCEPTED until then.
	RulesPending bool `json:"rules_pending,omitempty" binding:"optional"`
	// TimeoutUntil and TimeoutReason are set while the caller is timed out:
	// they can read but not post, react, speak or join Talks. The reason is
	// the moderator's, and the caller's own to see.
	TimeoutUntil  *time.Time `json:"timeout_until,omitempty" binding:"optional" format:"date-time"`
	TimeoutReason string     `json:"timeout_reason,omitempty" binding:"optional"`
	// NewMember is set while the caller is too new to write, react or speak
	// (raid protection): per action the moment it opens up.
	NewMember *NewMemberInfo `json:"new_member,omitempty" binding:"optional"`
	// Channels lists every channel the caller may view with what they may do
	// there (bits).
	Channels []ChannelPermissions `json:"channels"`
	// Catalog lists every permission bit.
	Catalog []perm.Info `json:"catalog"`
	// MaxUploadMB is the largest file the caller may upload, in MB: their
	// roles' override or the global limit, never above the server's ceiling.
	// It changes with permissions_changed.
	MaxUploadMB int `json:"max_upload_mb" minimum:"1"`
	// DirectMessagesEnabled is the community's switch for direct messages;
	// CanStartDirectMessages says whether the caller may start them and write
	// in them now (the switch is on, they hold Send direct messages, and no
	// timeout or pending rules apply).
	DirectMessagesEnabled  bool `json:"direct_messages_enabled" binding:"optional"`
	CanStartDirectMessages bool `json:"can_start_direct_messages" binding:"optional"`
}

// myPermissions handles GET /api/permissions.
//
// @Summary Own permissions
// @Description The caller's server-wide permissions and, per viewable channel, what they may do there. Refetch after a permissions_changed event.
// @ID getPermissions
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Success 200 {object} PermissionsInfo "Permissions."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/permissions [get]
func (h *Handler) myPermissions(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	sub := user.Subject()
	channels := []ChannelPermissions{}
	for id, p := range snap.ChannelPermissions(sub) {
		channels = append(channels, ChannelPermissions{ChannelID: id, Permissions: p})
	}
	slices.SortFunc(channels, func(a, b ChannelPermissions) int { return bytes.Compare(a.ChannelID[:], b.ChannelID[:]) })
	info := PermissionsInfo{
		Owner:       snap.IsOwner(sub),
		Permissions: snap.Global(sub),
		RoleIDs:     httpx.NonNil(snap.RoleIDs(user.ID)),
		TopPosition: min(snap.TopPosition(sub), maxReportedPosition),
		Channels:    channels,
		Catalog:     perm.Catalog,
		MaxUploadMB: snap.UploadMB(sub, h.Perms.UploadCeilingMB()),
	}
	info.DirectMessagesEnabled, info.CanStartDirectMessages = snap.DirectMessagesEnabled(), snap.CanStartDM(sub)
	info.RulesPending = snap.RulesPending(user.ID)
	if t, ok := snap.Timeout(user.ID); ok {
		info.TimeoutUntil, info.TimeoutReason = &t.Until, t.Reason
	}
	info.NewMember = newMemberInfo(snap.NewMemberWaits(user.ID), time.Now())
	httpx.WriteJSON(w, http.StatusOK, info)
	return nil
}

// newMemberInfo turns the remaining waits into the moments they end; nil when
// nothing holds the member back.
func newMemberInfo(w perm.NewMemberWaits, now time.Time) *NewMemberInfo {
	at := func(d time.Duration) *time.Time {
		if d <= 0 {
			return nil
		}
		t := now.Add(d).Truncate(time.Second).Add(time.Second)
		return &t
	}
	info := NewMemberInfo{WriteAt: at(w.Write), ReactAt: at(w.React), SpeakAt: at(w.Speak)}
	if info == (NewMemberInfo{}) {
		return nil
	}
	return &info
}

// maxReportedPosition is what the Owner's unlimited position is reported as.
const maxReportedPosition = 1 << 20

// UploadLimitInput is the body of PUT /api/admin/upload-limit.
type UploadLimitInput struct {
	// MaxUploadMB is the new global limit; null returns to the server's ceiling.
	MaxUploadMB *int `json:"max_upload_mb" minimum:"1" extensions:"x-nullable"`
}

// getUploadLimit handles GET /api/admin/upload-limit.
//
// @Summary Read the global upload limit
// @Description Owner only. The effective global limit and the operator's ceiling (MAX_UPLOAD_SIZE_MB).
// @ID getUploadLimit
// @Tags Roles
// @Produce json
// @Security cookieAuth
// @Success 200 {object} perm.UploadSettings "Upload limit."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/upload-limit [get]
func (h *Handler) getUploadLimit(w http.ResponseWriter, r *http.Request) error {
	st, err := h.Perms.UploadSettings(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}

// setUploadLimit handles PUT /api/admin/upload-limit.
//
// @Summary Set the global upload limit
// @Description Owner only. Capped by the operator's ceiling (MAX_UPLOAD_SIZE_MB); null returns to the ceiling. Roles may override it per role. Broadcasts permissions_changed so clients refetch their effective limit.
// @ID setUploadLimit
// @Tags Roles
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body UploadLimitInput true "New limit."
// @Success 200 {object} perm.UploadSettings "Upload limit."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may do this, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/upload-limit [put]
func (h *Handler) setUploadLimit(w http.ResponseWriter, r *http.Request) error {
	var in UploadLimitInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	st, err := h.Perms.SetGlobalUploadMB(r.Context(), auth.UserFrom(r.Context()).Subject(), in.MaxUploadMB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, st)
	return nil
}
