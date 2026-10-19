package bot

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// BotCommands is a list of definitions.
type BotCommands []CommandInput

// botList handles GET /api/bot/v1/commands.
//
// @Summary List the bot's commands
// @ID botListCommands
// @Tags Bot API
// @Produce json
// @Security botAuth
// @Success 200 {array} Command "The bot's commands."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/commands [get]
func (c *Commands) botList(w http.ResponseWriter, r *http.Request) error {
	list, err := ListCommands(r.Context(), c.DB, auth.BotFrom(r.Context()).BotID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

func cleanAll(in BotCommands) (BotCommands, error) {
	out := make(BotCommands, len(in))
	for i, d := range in {
		var err error
		if out[i], err = d.Clean(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// botReplace handles PUT /api/bot/v1/commands.
//
// @Summary Replace the bot's commands
// @Description Bulk overwrite as in Discord: adds and updates the given commands by name and removes the others. An admin's disabled flag on a command stays. At most 50 commands, 10 options each. Members see the commands in every channel the bot can view and send in.
// @ID botReplaceCommands
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param body body BotCommands true "The complete list of commands."
// @Success 200 {array} Command "The bot's commands."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/commands [put]
func (c *Commands) botReplace(w http.ResponseWriter, r *http.Request) error {
	var in BotCommands
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	clean, err := cleanAll(in)
	if err != nil {
		return err
	}
	list, err := ReplaceCommands(r.Context(), c.DB, auth.BotFrom(r.Context()).BotID, clean)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, list)
	return nil
}

// botCreate handles POST /api/bot/v1/commands.
//
// @Summary Register a command
// @Description Creates the command, or updates the one with the same name (keeping an admin's disabled flag).
// @ID botCreateCommand
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param body body CommandInput true "The command."
// @Success 200 {object} Command "The command."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 409 {object} httpx.DiscordError "The bot has the maximum number of commands."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/commands [post]
func (c *Commands) botCreate(w http.ResponseWriter, r *http.Request) error {
	var in CommandInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	in, err := in.Clean()
	if err != nil {
		return err
	}
	cmd, err := UpsertCommand(r.Context(), c.DB, auth.BotFrom(r.Context()).BotID, in)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, cmd)
	return nil
}

// botDelete handles DELETE /api/bot/v1/commands/{commandID}.
//
// @Summary Delete a command
// @ID botDeleteCommand
// @Tags Bot API
// @Security botAuth
// @Param commandID path string true "Command ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 404 {object} httpx.DiscordError "Unknown command."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Router /api/bot/v1/commands/{commandID} [delete]
func (c *Commands) botDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "commandID")
	if err != nil {
		return err
	}
	if err := DeleteCommand(r.Context(), c.DB, auth.BotFrom(r.Context()).BotID, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
