package webhook

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// mountOutgoing registers the outgoing webhook management routes.
func (h *Handler) mountOutgoing(r chi.Router) {
	r.Get("/channels/{id}/outgoing-webhooks", httpx.Handle(h.listOutgoing))
	r.Post("/channels/{id}/outgoing-webhooks", httpx.Handle(h.createOutgoing))
	r.Get("/outgoing-webhooks", httpx.Handle(h.listServerOutgoing))
	r.Post("/outgoing-webhooks", httpx.Handle(h.createServerOutgoing))
	r.Patch("/outgoing-webhooks/{id}", httpx.Handle(h.updateOutgoing))
	r.Post("/outgoing-webhooks/{id}/secret", httpx.Handle(h.rotateOutgoingSecret))
	r.Post("/outgoing-webhooks/{id}/test", httpx.Handle(h.testOutgoing))
	r.Get("/outgoing-webhooks/{id}/deliveries", httpx.Handle(h.outgoingDeliveries))
	r.Delete("/outgoing-webhooks/{id}", httpx.Handle(h.deleteOutgoing))
}

// CreateOutgoingRequest is the body of creating an outgoing webhook.
type CreateOutgoingRequest struct {
	Name string `json:"name" maxLength:"80"`
	// URL receives the events: http or https, public addresses only unless
	// the operator allowed more (WEBHOOK_ALLOW_PRIVATE_TARGETS).
	URL    string   `json:"url" maxLength:"2048"`
	Events []string `json:"events" enums:"MESSAGE_CREATE,MESSAGE_UPDATE,MESSAGE_DELETE,MESSAGE_REACTIONS_UPDATE,VOICE_STATE_UPDATE,MEMBER_ADD"`
}

// UpdateOutgoingRequest changes an outgoing webhook; omitted fields stay.
// Switching it on again clears an automatic switch-off and the failure count.
type UpdateOutgoingRequest struct {
	Name    *string   `json:"name,omitempty" binding:"optional" maxLength:"80"`
	URL     *string   `json:"url,omitempty" binding:"optional" maxLength:"2048"`
	Events  *[]string `json:"events,omitempty" binding:"optional"`
	Enabled *bool     `json:"enabled,omitempty" binding:"optional"`
}

// loadOutgoing finds the webhook of the path and checks the caller may manage
// it: the Owner for a server-wide one, otherwise the same rules as for
// incoming webhooks.
func (h *Handler) loadOutgoing(r *http.Request) (*outgoingRec, error) {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return nil, err
	}
	rec, err := getOutgoing(r.Context(), h.DB, id)
	if err != nil {
		return nil, err
	}
	if rec.ChannelID == nil {
		return rec, requireOwner(r)
	}
	snap, err := h.authorize(r, *rec.ChannelID, 0)
	if err != nil {
		if ae, ok := httpx.AsAPIError(err); ok && ae.Status == http.StatusNotFound {
			return nil, errOutgoingNotFound
		}
		return nil, err
	}
	if snap == nil {
		return rec, nil
	}
	return rec, hierarchy(r, snap, rec.createdBy)
}

func requireOwner(r *http.Request) error {
	if !auth.UserFrom(r.Context()).Subject().Owner {
		return httpx.ErrForbidden("only the Owner can do this")
	}
	return nil
}

func (h *Handler) present(ctx context.Context, rec *outgoingRec) Outgoing {
	out := rec.Outgoing
	out.AccessOK = h.Outgoing.mayDeliver(ctx, rec)
	return out
}

func (h *Handler) reload(ctx context.Context) {
	if err := h.Outgoing.Reload(ctx); err != nil {
		slog.Warn("outgoing webhooks: reload", "err", err)
	}
}

func (h *Handler) writeOutgoingList(w http.ResponseWriter, r *http.Request, channel *uuid.UUID) error {
	recs, err := listOutgoing(r.Context(), h.DB, channel)
	if err != nil {
		return err
	}
	out := make([]Outgoing, len(recs))
	for i, rec := range recs {
		out[i] = h.present(r.Context(), rec)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// listOutgoing handles GET /api/admin/channels/{id}/outgoing-webhooks.
//
// @Summary List a channel's outgoing webhooks
// @Description Needs Manage webhooks and View channel in that channel. Secrets are never listed.
// @ID listOutgoingWebhooks
// @Tags Webhooks
// @Produce json
// @Security cookieAuth
// @Param id path string true "Channel ID." Format(uuid)
// @Success 200 {array} Outgoing "Outgoing webhooks, oldest first."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the channel does not exist or is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/outgoing-webhooks [get]
func (h *Handler) listOutgoing(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := h.authorize(r, ch, 0); err != nil {
		return err
	}
	return h.writeOutgoingList(w, r, &ch)
}

// listServerOutgoing handles GET /api/admin/outgoing-webhooks.
//
// @Summary List the server-wide outgoing webhooks
// @Description Owner only.
// @ID listServerOutgoingWebhooks
// @Tags Webhooks
// @Produce json
// @Security cookieAuth
// @Success 200 {array} Outgoing "Server-wide outgoing webhooks, oldest first."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/outgoing-webhooks [get]
func (h *Handler) listServerOutgoing(w http.ResponseWriter, r *http.Request) error {
	if err := requireOwner(r); err != nil {
		return err
	}
	return h.writeOutgoingList(w, r, nil)
}

func (h *Handler) createIn(w http.ResponseWriter, r *http.Request, channel *uuid.UUID) error {
	var req CreateOutgoingRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name, err := cleanName("name", req.Name)
	if err != nil {
		return err
	}
	u, err := h.Policy.ValidateURL(req.URL)
	if err != nil {
		return httpx.ErrInvalidInput("url must be an http or https address that is allowed as a target")
	}
	events, err := validEvents(req.Events, channel == nil)
	if err != nil {
		return err
	}
	rec, secret, err := createOutgoing(r.Context(), h.DB, channel, auth.UserFrom(r.Context()).ID, name, u.String(), events)
	if err != nil {
		return err
	}
	h.reload(r.Context())
	httpx.WriteJSON(w, http.StatusCreated, CreatedOutgoing{Outgoing: h.present(r.Context(), rec), Secret: secret})
	return nil
}

// createOutgoing handles POST /api/admin/channels/{id}/outgoing-webhooks.
//
// @Summary Create an outgoing webhook
// @Description Needs Manage webhooks and View channel in that channel. The answer carries the signing secret, shown only once. Events of that channel are POSTed to the URL as signed JSON; see doc/webhooks.md. At most 10 per channel.
// @ID createOutgoingWebhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Channel ID." Format(uuid)
// @Param body body CreateOutgoingRequest true "Name, URL and events (MESSAGE_CREATE, MESSAGE_UPDATE, MESSAGE_DELETE, MESSAGE_REACTIONS_UPDATE, VOICE_STATE_UPDATE)."
// @Success 201 {object} CreatedOutgoing "Created."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): bad name, URL (also: a forbidden address) or events."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the channel does not exist or is hidden from the caller."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the maximum number of outgoing webhooks is reached."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/channels/{id}/outgoing-webhooks [post]
func (h *Handler) createOutgoing(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := h.authorize(r, ch, 0); err != nil {
		return err
	}
	return h.createIn(w, r, &ch)
}

// createServerOutgoing handles POST /api/admin/outgoing-webhooks.
//
// @Summary Create a server-wide outgoing webhook
// @Description Owner only. Receives the subscribed events of every channel plus MEMBER_ADD. The answer carries the signing secret, shown only once.
// @ID createServerOutgoingWebhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body CreateOutgoingRequest true "Name, URL and events (also MEMBER_ADD)."
// @Success 201 {object} CreatedOutgoing "Created."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner, or cross-origin request rejected."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the maximum number of outgoing webhooks is reached."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/outgoing-webhooks [post]
func (h *Handler) createServerOutgoing(w http.ResponseWriter, r *http.Request) error {
	if err := requireOwner(r); err != nil {
		return err
	}
	return h.createIn(w, r, nil)
}

// updateOutgoing handles PATCH /api/admin/outgoing-webhooks/{id}.
//
// @Summary Change an outgoing webhook
// @Description Rename it, change its URL or events, switch it on or off. Switching it on clears an automatic switch-off. Changing the URL makes the caller the webhook's creator (deliveries follow their rights); rotating the secret, renaming and switching on or off never change the creator. Needs Manage webhooks in its channel (the Owner for server-wide ones), and a highest role not below the creator's.
// @ID updateOutgoingWebhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Outgoing webhook ID." Format(uuid)
// @Param body body UpdateOutgoingRequest true "Fields to change."
// @Success 200 {object} Outgoing "Updated."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such outgoing webhook."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/outgoing-webhooks/{id} [patch]
func (h *Handler) updateOutgoing(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.loadOutgoing(r)
	if err != nil {
		return err
	}
	var req UpdateOutgoingRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	change := outgoingChange{enabled: req.Enabled}
	if req.Name != nil {
		name, err := cleanName("name", *req.Name)
		if err != nil {
			return err
		}
		change.name = &name
	}
	if req.URL != nil {
		u, err := h.Policy.ValidateURL(*req.URL)
		if err != nil {
			return httpx.ErrInvalidInput("url must be an http or https address that is allowed as a target")
		}
		s := u.String()
		change.url = &s
		// Whoever points the events somewhere becomes responsible for who
		// may see them: deliveries follow their rights from now on.
		editor := auth.UserFrom(r.Context()).ID
		change.owner = &editor
	}
	if req.Events != nil {
		if change.events, err = validEvents(*req.Events, rec.ChannelID == nil); err != nil {
			return err
		}
	}
	if err := updateOutgoing(r.Context(), h.DB, rec.ID, change); err != nil {
		return err
	}
	h.reload(r.Context())
	cur, err := getOutgoing(r.Context(), h.DB, rec.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, h.present(r.Context(), cur))
	return nil
}

// rotateOutgoingSecret handles POST /api/admin/outgoing-webhooks/{id}/secret.
//
// @Summary Rotate an outgoing webhook's secret
// @Description The old secret stops signing at once. The answer carries the new secret, shown only once. The creator stays as it is.
// @ID rotateOutgoingWebhookSecret
// @Tags Webhooks
// @Produce json
// @Security cookieAuth
// @Param id path string true "Outgoing webhook ID." Format(uuid)
// @Success 200 {object} CreatedOutgoing "New secret."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such outgoing webhook."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/outgoing-webhooks/{id}/secret [post]
func (h *Handler) rotateOutgoingSecret(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.loadOutgoing(r)
	if err != nil {
		return err
	}
	secret, err := rotateSecret(r.Context(), h.DB, rec.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, CreatedOutgoing{Outgoing: h.present(r.Context(), rec), Secret: secret})
	return nil
}

// testOutgoing handles POST /api/admin/outgoing-webhooks/{id}/test.
//
// @Summary Send a test event
// @Description Delivers a PING event once, right now, and answers with the attempt. It does not retry and does not count towards the automatic switch-off. Works while the webhook is switched off.
// @ID testOutgoingWebhook
// @Tags Webhooks
// @Produce json
// @Security cookieAuth
// @Param id path string true "Outgoing webhook ID." Format(uuid)
// @Success 200 {object} Delivery "The attempt (success or the failure code)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such outgoing webhook."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/outgoing-webhooks/{id}/test [post]
func (h *Handler) testOutgoing(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.loadOutgoing(r)
	if err != nil {
		return err
	}
	if !h.Outgoing.mayDeliver(r.Context(), rec) {
		return httpx.ErrForbidden("the creator of this webhook no longer has the rights it needs")
	}
	if ok, _ := h.testLimiter.Allow(rec.ID.String()); !ok {
		return httpx.ErrRateLimited()
	}
	del, err := h.Outgoing.SendTest(r.Context(), rec)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, del)
	return nil
}

// outgoingDeliveries handles GET /api/admin/outgoing-webhooks/{id}/deliveries.
//
// @Summary Recent deliveries of an outgoing webhook
// @Description The last 50 attempts, newest first. Only status codes and short error codes are kept, never the receiver's response.
// @ID listOutgoingWebhookDeliveries
// @Tags Webhooks
// @Produce json
// @Security cookieAuth
// @Param id path string true "Outgoing webhook ID." Format(uuid)
// @Success 200 {array} Delivery "Attempts."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such outgoing webhook."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/outgoing-webhooks/{id}/deliveries [get]
func (h *Handler) outgoingDeliveries(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.loadOutgoing(r)
	if err != nil {
		return err
	}
	list, err := listDeliveries(r.Context(), h.DB, rec.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// deleteOutgoing handles DELETE /api/admin/outgoing-webhooks/{id}.
//
// @Summary Delete an outgoing webhook
// @Description Events still waiting to be delivered to it are dropped.
// @ID deleteOutgoingWebhook
// @Tags Webhooks
// @Security cookieAuth
// @Param id path string true "Outgoing webhook ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission or hierarchy, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such outgoing webhook."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/outgoing-webhooks/{id} [delete]
func (h *Handler) deleteOutgoing(w http.ResponseWriter, r *http.Request) error {
	rec, err := h.loadOutgoing(r)
	if err != nil {
		return err
	}
	if err := deleteOutgoing(r.Context(), h.DB, rec.ID); err != nil {
		return err
	}
	h.reload(r.Context())
	w.WriteHeader(http.StatusNoContent)
	return nil
}
