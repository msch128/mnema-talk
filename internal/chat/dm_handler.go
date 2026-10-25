package chat

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func (h *Handler) mountDMs(r chi.Router) {
	r.Get("/dms", httpx.Handle(h.listDMs))
	r.Get("/dms/{id}", httpx.Handle(h.getDM))
	r.Delete("/dms/{id}/members/{user}", httpx.Handle(h.leaveDM))
	r.Get("/users/me/privacy", httpx.Handle(h.getPrivacy))
	r.Put("/users/me/privacy", httpx.Handle(h.setPrivacy))
	h.mountDMPrefs(r)
	starts := r
	if h.DMLimit != nil {
		starts = r.With(h.DMLimit.By(auth.UserKey))
	}
	starts.Post("/dms", httpx.Handle(h.createDM))
	starts.Post("/dms/{id}/members", httpx.Handle(h.addDMMember))
}

// DMStarted is what the other members of a new conversation, and the starter's
// other sessions, hear: the conversation as its member sees it at the start.
// It is the payload of dm_created.
type DMStarted = DirectMessage

// DMMemberChange is the payload of dm_member_added and dm_member_removed.
type DMMemberChange struct {
	DMID uuid.UUID `json:"dm_id" format:"uuid"`
	// Member is the member who joined or left.
	Member DMMember `json:"member"`
}

// canStartDMs requires the caller to hold Send direct messages with the
// community switch on, answering the generic refusal otherwise.
func (h *Handler) canStartDMs(r *http.Request) error {
	if h.Perms == nil {
		return nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	if !snap.CanStartDM(auth.UserFrom(r.Context()).Subject()) {
		return errDMStart
	}
	return nil
}

// listDMs handles GET /api/dms.
//
// @Summary List direct messages
// @Description The caller's direct message conversations, the most recently active first (at most 500), with members, unread and mention counts. Conversations are not channels: they appear in no channel list. Messages are read and written under the conversation's id in the message routes, which only members can use. Moderators cannot list or open anybody else's conversations. Needs the direct_messages feature (GET /api/health).
// @ID listDirectMessages
// @Tags Direct messages
// @Produce json
// @Security cookieAuth
// @Success 200 {array} DirectMessage "The caller's conversations."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms [get]
func (h *Handler) listDMs(w http.ResponseWriter, r *http.Request) error {
	dms, err := ListDMs(r.Context(), h.DB, auth.UserFrom(r.Context()).ID, nil)
	if err != nil {
		return err
	}
	h.markCanSend(r, dms)
	if dms == nil {
		dms = []DirectMessage{}
	}
	httpx.WriteJSON(w, http.StatusOK, dms)
	return nil
}

// markCanSend fills CanSend from the caller's permissions.
func (h *Handler) markCanSend(r *http.Request, dms []DirectMessage) {
	can := h.Perms == nil
	if !can {
		if snap, err := h.Perms.Snapshot(r.Context()); err == nil {
			can = snap.CanStartDM(auth.UserFrom(r.Context()).Subject())
		}
	}
	for i := range dms {
		dms[i].CanSend = can
	}
}

// getDM handles GET /api/dms/{id}.
//
// @Summary Get a direct message
// @Description One conversation of the caller. Anyone who is not in it, the Owner included, gets 404.
// @ID getDirectMessage
// @Tags Direct messages
// @Produce json
// @Security cookieAuth
// @Param id path string true "Conversation ID." Format(uuid)
// @Success 200 {object} DirectMessage "The conversation."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms/{id} [get]
func (h *Handler) getDM(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	dm, err := h.loadDM(r, id)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, dm)
	return nil
}

func (h *Handler) loadDM(r *http.Request, id uuid.UUID) (DirectMessage, error) {
	dms, err := ListDMs(r.Context(), h.DB, auth.UserFrom(r.Context()).ID, &id)
	if err != nil {
		return DirectMessage{}, err
	}
	if len(dms) == 0 {
		return DirectMessage{}, errDMNotFound
	}
	h.markCanSend(r, dms)
	return dms[0], nil
}

// CreateDMRequest is the body of POST /api/dms.
type CreateDMRequest struct {
	// UserIDs are the other members: one for a conversation with one person,
	// two to nine for a group.
	UserIDs []uuid.UUID `json:"user_ids" minItems:"1" maxItems:"9" format:"uuid"`
}

// createDM handles POST /api/dms.
//
// @Summary Start a direct message
// @Description With one other member: that conversation, started now (201) or the existing one (200). With two to nine: a new group (201). Needs Send direct messages and the community's direct messages on. Every refusal about the other members (a block in either direction, their direct message setting, a disabled account, a bot, an unknown id) is the same 403, so none can be probed. At most 10 new conversations per member per 24 hours and 3 tries per minute, and 100 group conversations per member. The first messages are capped (see doc/architecture.md). Sends dm_created to all members' sessions.
// @ID createDirectMessage
// @Tags Direct messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body CreateDMRequest true "Request body."
// @Success 200 {object} DirectMessage "The existing conversation with that member."
// @Success 201 {object} DirectMessage "The new conversation."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the conversation cannot be started (always the same text)."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the caller is in 100 group conversations."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or 10 conversations started in 24 hours."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms [post]
func (h *Handler) createDM(w http.ResponseWriter, r *http.Request) error {
	var req CreateDMRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	dm, created, err := h.StartDM(r, req.UserIDs)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, dm)
	return nil
}

// announceDM tells every member's sessions about a conversation they are in.
func (h *Handler) announceDM(dm DirectMessage) {
	if h.Events == nil {
		return
	}
	ids := make([]uuid.UUID, len(dm.Members))
	for i, m := range dm.Members {
		ids[i] = m.ID
	}
	// Counts are the starter's to see; the others fetch their own.
	dm = dm.forOthers()
	h.Events.SendToUsers(ids, events.DMCreated, dm)
}

// AddDMMemberRequest is the body of POST /api/dms/{id}/members.
type AddDMMemberRequest struct {
	UserID uuid.UUID `json:"user_id" format:"uuid"`
}

// addDMMember handles POST /api/dms/{id}/members.
//
// @Summary Add a member to a group direct message
// @Description Only group conversations take members, up to 10 in all, and only a member of it can add. The new member can read the whole conversation. The same single 403 as for starting a conversation covers a block in either direction between the adder and the new member, their direct message setting, a disabled account and a bot. Sends dm_created to the new member and dm_member_added to the others.
// @ID addDirectMessageMember
// @Tags Direct messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Conversation ID." Format(uuid)
// @Param request body AddDMMemberRequest true "Request body."
// @Success 200 {object} DirectMessage "The conversation, with the new member."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the member cannot be added (always the same text)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the group is full."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms/{id}/members [post]
func (h *Handler) addDMMember(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req AddDMMemberRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if _, err := h.loadDM(r, id); err != nil {
		return err
	}
	if err := h.canStartDMs(r); err != nil {
		return err
	}
	me := auth.UserFrom(r.Context()).ID
	added, err := AddDMMember(r.Context(), h.DB, id, me, req.UserID)
	if err != nil {
		return err
	}
	dm, err := h.loadDM(r, id)
	if err != nil {
		return err
	}
	if added {
		h.Perms.Invalidate()
		h.announceAdded(dm, req.UserID)
	}
	httpx.WriteJSON(w, http.StatusOK, dm)
	return nil
}

func (h *Handler) announceAdded(dm DirectMessage, added uuid.UUID) {
	if h.Events == nil {
		return
	}
	var others []uuid.UUID
	var joined DMMember
	for _, m := range dm.Members {
		if m.ID == added {
			joined = m
		} else {
			others = append(others, m.ID)
		}
	}
	dm = dm.forOthers()
	h.Events.SendToUsers([]uuid.UUID{added}, events.DMCreated, dm)
	h.Events.SendToUsers(others, events.DMMemberAdded, DMMemberChange{DMID: dm.ID, Member: joined})
}

// leaveDM handles DELETE /api/dms/{id}/members/@me.
//
// @Summary Leave a group direct message
// @Description Takes the caller out of a group conversation; they lose its history. When nobody is left the conversation and its messages and attachments are deleted. A conversation with one other person cannot be left (400): block the person instead. Sends dm_member_removed to the remaining members and the leaver's sessions.
// @ID leaveDirectMessage
// @Tags Direct messages
// @Security cookieAuth
// @Param id path string true "Conversation ID." Format(uuid)
// @Param user path string true "Must be the literal @me (or the caller's own id): members can only remove themselves."
// @Success 204 "Left."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/dms/{id}/members/{user} [delete]
func (h *Handler) leaveDM(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	if who := chi.URLParam(r, "user"); who != "@me" && who != user.ID.String() {
		return httpx.ErrInvalidInput("you can only remove yourself: use @me")
	}
	remaining, keys, err := LeaveDM(r.Context(), h.DB, id, user.ID)
	if err != nil {
		return err
	}
	h.Perms.Invalidate()
	h.deleteObjects(r.Context(), keys)
	if h.Events != nil {
		left := DMMember{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName}
		h.Events.SendToUsers(append(remaining, user.ID), events.DMMemberRemoved, DMMemberChange{DMID: id, Member: left})
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Privacy is the caller's choice about who may start direct messages with them.
type Privacy struct {
	// DMPolicy is everyone (the default) or nobody. It only decides who can
	// start a conversation with the caller or add them to a group; existing
	// conversations go on.
	DMPolicy string `json:"dm_policy" enums:"everyone,nobody"`
}

// getPrivacy handles GET /api/users/me/privacy.
//
// @Summary Read the privacy settings
// @Description Who may start direct messages with the caller.
// @ID getPrivacy
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Privacy "The settings."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/privacy [get]
func (h *Handler) getPrivacy(w http.ResponseWriter, r *http.Request) error {
	policy, err := GetDMPolicy(r.Context(), h.DB, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, Privacy{DMPolicy: policy})
	return nil
}

// setPrivacy handles PUT /api/users/me/privacy.
//
// @Summary Set the privacy settings
// @Description Sets who may start direct messages with the caller: everyone or nobody.
// @ID setPrivacy
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body Privacy true "Request body."
// @Success 200 {object} Privacy "The settings."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/privacy [put]
func (h *Handler) setPrivacy(w http.ResponseWriter, r *http.Request) error {
	var req Privacy
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := SetDMPolicy(r.Context(), h.DB, auth.UserFrom(r.Context()).ID, req.DMPolicy); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, req)
	return nil
}
