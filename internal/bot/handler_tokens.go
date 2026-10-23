package bot

import (
	"net/http"
	"slices"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// TokenRequest is the body of issuing a token.
type TokenRequest struct {
	Name string `json:"name,omitempty" binding:"optional" maxLength:"32"`
	// ExpiresAt is optional; without it the token never expires.
	ExpiresAt *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
}

// NewTokenResponse carries a token's secret, shown once.
type NewTokenResponse struct {
	Token
	// Secret is the API token. It is shown only now; only its hash is kept.
	Secret string `json:"secret"`
}

// listTokens handles GET /api/admin/bots/{id}/tokens.
//
// @Summary List a bot's tokens
// @Description Never contains the secrets: name, last characters, state, expiry and last use.
// @ID listBotTokens
// @Tags Bots
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Success 200 {array} Token "Tokens, newest first."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or the bot's highest role is not below yours."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/tokens [get]
func (h *Handler) listTokens(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	ts, err := Tokens(r.Context(), h.DB, b.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, ts)
	return nil
}

func cleanTokenRequest(r *http.Request) (string, *time.Time, error) {
	var req TokenRequest
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return "", nil, err
		}
	}
	name, err := httpx.CleanName("name", req.Name, 32, false)
	if err != nil {
		return "", nil, err
	}
	if req.ExpiresAt != nil {
		if err := CheckExpiry(*req.ExpiresAt, time.Now()); err != nil {
			return "", nil, err
		}
	}
	return name, req.ExpiresAt, nil
}

// createToken handles POST /api/admin/bots/{id}/tokens.
//
// @Summary Issue a token
// @Description The secret is in the answer and shown only once; only a SHA-256 hash is stored. Optional expiry. At most 10 active tokens per bot.
// @ID createBotToken
// @Tags Bots
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param body body TokenRequest false "Name and expiry."
// @Success 201 {object} NewTokenResponse "Issued."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT)."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or the bot's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many active tokens."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/tokens [post]
func (h *Handler) createToken(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	name, exp, err := cleanTokenRequest(r)
	if err != nil {
		return err
	}
	t, secret, err := IssueToken(r.Context(), h.DB, b.ID, name, exp, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, NewTokenResponse{Token: t, Secret: secret})
	return nil
}

// regenerateToken handles POST /api/admin/bots/{id}/tokens/{tokenID}/regenerate.
//
// @Summary Regenerate a token
// @Description Issues a new token with the same name and expiry and revokes the old one at once (its gateway connections close).
// @ID regenerateBotToken
// @Tags Bots
// @Produce json
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param tokenID path string true "Token ID." Format(uuid)
// @Success 201 {object} NewTokenResponse "Issued; the secret is shown once."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or the bot's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot or token."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many active tokens."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/tokens/{tokenID}/regenerate [post]
func (h *Handler) regenerateToken(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	tokenID, err := httpx.PathUUID(r, "tokenID")
	if err != nil {
		return err
	}
	ts, err := Tokens(r.Context(), h.DB, b.ID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(ts, func(t Token) bool { return t.ID == tokenID })
	if i < 0 {
		return errTokenNotFound
	}
	old := ts[i]
	if err := RevokeToken(r.Context(), h.DB, b.ID, tokenID); err != nil {
		return err
	}
	if h.OnRevoke != nil {
		h.OnRevoke(b.ID, &tokenID)
	}
	t, secret, err := IssueToken(r.Context(), h.DB, b.ID, old.Name, old.ExpiresAt, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, NewTokenResponse{Token: t, Secret: secret})
	return nil
}

// revokeToken handles DELETE /api/admin/bots/{id}/tokens/{tokenID}.
//
// @Summary Revoke a token
// @Description Takes effect at once; open gateway connections that used the token close.
// @ID revokeBotToken
// @Tags Bots
// @Security cookieAuth
// @Param id path string true "Bot ID." Format(uuid)
// @Param tokenID path string true "Token ID." Format(uuid)
// @Success 204 "Revoked."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage bots permission, or the bot's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such bot or token."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/bots/{id}/tokens/{tokenID} [delete]
func (h *Handler) revokeToken(w http.ResponseWriter, r *http.Request) error {
	b, err := h.loadActing(r)
	if err != nil {
		return err
	}
	tokenID, err := httpx.PathUUID(r, "tokenID")
	if err != nil {
		return err
	}
	if err := RevokeToken(r.Context(), h.DB, b.ID, tokenID); err != nil {
		return err
	}
	if h.OnRevoke != nil {
		h.OnRevoke(b.ID, &tokenID)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
