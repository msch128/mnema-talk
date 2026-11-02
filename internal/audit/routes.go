package audit

import (
	"net/http"
	"strings"
)

// AdminPrefix is where the audited routes live.
const AdminPrefix = "/api/admin"

// Resource names what a snapshot is taken of before a change.
type Resource string

const (
	ResNone       Resource = ""
	ResRole       Resource = "role"
	ResChannel    Resource = "channel"
	ResCategory   Resource = "category"
	ResWebhook    Resource = "webhook"
	ResOutgoing   Resource = "outgoing_webhook"
	ResUser       Resource = "user"
	ResInvite     Resource = "invite"
	ResChannelOvw Resource = "channel_overwrites"
	ResCatOvw     Resource = "category_overwrites"
	ResSettings   Resource = "settings"
	ResBot        Resource = "bot"
	ResOnboarding Resource = "onboarding"
	ResVanity     Resource = "vanity"
	ResEmoji      Resource = "emoji"
)

// Route describes one mutating admin route.
type Route struct {
	Method  string
	Pattern string // below /api/admin, with {name} segments
	Action  string
	// TargetType is what the entry's target is; the target id comes from the
	// path parameter IDParam, or from the response's "id" for creations.
	TargetType string
	IDParam    string
	// Snapshot is the resource read before the change (ResNone: none).
	Snapshot Resource
	// Skip marks mutating-looking routes that change nothing worth auditing.
	Skip bool
	// NoBody keeps the request body out of the entry (uploads are not JSON
	// anyway).
	NoBody bool
}

// Routes is every mutating route under /api/admin. A test walks the router
// and fails when a new route is missing here; the middleware still records
// unknown ones under a generic action, so nothing escapes the log.
var Routes = []Route{
	{Method: "POST", Pattern: "/invites", Action: "invite.create", TargetType: "invite"},
	{Method: "PUT", Pattern: "/vanity-link", Action: "invite.vanity_link", TargetType: "settings", Snapshot: ResVanity},
	{Method: "PATCH", Pattern: "/invites/{id}", Action: "invite.update", TargetType: "invite", IDParam: "id", Snapshot: ResInvite},
	{Method: "DELETE", Pattern: "/invites/{id}", Action: "invite.revoke", TargetType: "invite", IDParam: "id", Snapshot: ResInvite},

	{Method: "POST", Pattern: "/users/{id}/disable", Action: "user.disable", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/enable", Action: "user.enable", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/sessions/revoke", Action: "user.sessions_revoke", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "PUT", Pattern: "/users/{id}/status", Action: "user.status", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/password", Action: "user.password_set", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/password-reset", Action: "user.password_reset", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/two-factor/reset", Action: "user.two_factor_reset", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/login-lockout/clear", Action: "user.lockout_clear", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/kick", Action: "user.talk_kick", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/timeout", Action: "moderation.timeout", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "DELETE", Pattern: "/users/{id}/timeout", Action: "moderation.timeout_remove", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "POST", Pattern: "/users/{id}/ban", Action: "moderation.ban", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "DELETE", Pattern: "/users/{id}/ban", Action: "moderation.unban", TargetType: "user", IDParam: "id", Snapshot: ResUser},
	{Method: "PUT", Pattern: "/users/{id}/roles", Action: "user.roles_set", TargetType: "user", IDParam: "id", Snapshot: ResUser},

	{Method: "POST", Pattern: "/roles", Action: "role.create", TargetType: "role"},
	{Method: "PUT", Pattern: "/roles/order", Action: "role.reorder", TargetType: "role"},
	{Method: "PATCH", Pattern: "/roles/{id}", Action: "role.update", TargetType: "role", IDParam: "id", Snapshot: ResRole},
	{Method: "DELETE", Pattern: "/roles/{id}", Action: "role.delete", TargetType: "role", IDParam: "id", Snapshot: ResRole},

	{Method: "PUT", Pattern: "/channels/{id}/overwrites", Action: "channel.overwrites_set", TargetType: "channel", IDParam: "id", Snapshot: ResChannelOvw},
	{Method: "PUT", Pattern: "/categories/{id}/overwrites", Action: "category.overwrites_set", TargetType: "category", IDParam: "id", Snapshot: ResCatOvw},

	{Method: "POST", Pattern: "/channels", Action: "channel.create", TargetType: "channel"},
	{Method: "PATCH", Pattern: "/channels/{id}", Action: "channel.update", TargetType: "channel", IDParam: "id", Snapshot: ResChannel},
	{Method: "PUT", Pattern: "/channels/{id}/auto-delete", Action: "channel.auto_delete_set", TargetType: "channel", IDParam: "id", Snapshot: ResChannel},
	{Method: "DELETE", Pattern: "/channels/{id}", Action: "channel.delete", TargetType: "channel", IDParam: "id", Snapshot: ResChannel},
	{Method: "POST", Pattern: "/channels/{id}/duplicate", Action: "channel.duplicate", TargetType: "channel", IDParam: "id", Snapshot: ResChannel},
	{Method: "POST", Pattern: "/categories", Action: "category.create", TargetType: "category"},
	{Method: "PATCH", Pattern: "/categories/{id}", Action: "category.update", TargetType: "category", IDParam: "id", Snapshot: ResCategory},
	{Method: "DELETE", Pattern: "/categories/{id}", Action: "category.delete", TargetType: "category", IDParam: "id", Snapshot: ResCategory},
	{Method: "PUT", Pattern: "/layout", Action: "layout.apply", TargetType: "layout"},

	{Method: "POST", Pattern: "/channels/{id}/webhooks", Action: "webhook.create", TargetType: "webhook"},
	{Method: "PATCH", Pattern: "/webhooks/{id}", Action: "webhook.update", TargetType: "webhook", IDParam: "id", Snapshot: ResWebhook},
	{Method: "POST", Pattern: "/webhooks/{id}/token", Action: "webhook.regenerate", TargetType: "webhook", IDParam: "id", Snapshot: ResWebhook},
	{Method: "POST", Pattern: "/webhooks/{id}/avatar", Action: "webhook.avatar", TargetType: "webhook", IDParam: "id", Snapshot: ResWebhook, NoBody: true},
	{Method: "DELETE", Pattern: "/webhooks/{id}", Action: "webhook.delete", TargetType: "webhook", IDParam: "id", Snapshot: ResWebhook},
	{Method: "POST", Pattern: "/channels/{id}/outgoing-webhooks", Action: "outgoing_webhook.create", TargetType: "outgoing_webhook"},
	{Method: "PATCH", Pattern: "/outgoing-webhooks/{id}", Action: "outgoing_webhook.update", TargetType: "outgoing_webhook", IDParam: "id", Snapshot: ResOutgoing},
	{Method: "POST", Pattern: "/outgoing-webhooks/{id}/secret", Action: "outgoing_webhook.rotate_secret", TargetType: "outgoing_webhook", IDParam: "id", Snapshot: ResOutgoing},
	{Method: "POST", Pattern: "/outgoing-webhooks/{id}/test", Skip: true},
	{Method: "DELETE", Pattern: "/outgoing-webhooks/{id}", Action: "outgoing_webhook.delete", TargetType: "outgoing_webhook", IDParam: "id", Snapshot: ResOutgoing},

	{Method: "POST", Pattern: "/bots", Action: "bot.create", TargetType: "bot"},
	{Method: "PATCH", Pattern: "/bots/{id}", Action: "bot.update", TargetType: "bot", IDParam: "id", Snapshot: ResBot},
	{Method: "DELETE", Pattern: "/bots/{id}", Action: "bot.delete", TargetType: "bot", IDParam: "id", Snapshot: ResBot},
	{Method: "POST", Pattern: "/bots/{id}/avatar", Action: "bot.avatar", TargetType: "bot", IDParam: "id", Snapshot: ResBot, NoBody: true},
	{Method: "PUT", Pattern: "/bots/{id}/roles", Action: "bot.roles", TargetType: "bot", IDParam: "id", Snapshot: ResBot},
	{Method: "POST", Pattern: "/bots/{id}/tokens", Action: "bot.token_create", TargetType: "bot", IDParam: "id", Snapshot: ResBot},
	{Method: "POST", Pattern: "/bots/{id}/tokens/{tokenID}/regenerate", Action: "bot.token_regenerate", TargetType: "bot", IDParam: "id", Snapshot: ResBot},
	{Method: "DELETE", Pattern: "/bots/{id}/tokens/{tokenID}", Action: "bot.token_revoke", TargetType: "bot", IDParam: "id", Snapshot: ResBot},
	{Method: "PATCH", Pattern: "/bots/{id}/commands/{commandID}", Action: "bot.command_update", TargetType: "bot", IDParam: "id", Snapshot: ResBot},
	{Method: "DELETE", Pattern: "/bots/{id}/commands/{commandID}", Action: "bot.command_delete", TargetType: "bot", IDParam: "id", Snapshot: ResBot},

	// The import writes its own audit entry in the transaction that does the work.
	{Method: "POST", Pattern: "/import/discord/preview", Skip: true},
	{Method: "POST", Pattern: "/import/discord/apply", Skip: true},
	{Method: "POST", Pattern: "/import/discord/imports/{id}/undo", Skip: true},

	{Method: "PUT", Pattern: "/upload-limit", Action: "settings.upload_limit", TargetType: "settings", Snapshot: ResSettings},

	{Method: "PUT", Pattern: "/community/icon", Action: "settings.community_icon", TargetType: "settings", Snapshot: ResOnboarding, NoBody: true},
	{Method: "DELETE", Pattern: "/community/icon", Action: "settings.community_icon_remove", TargetType: "settings", Snapshot: ResOnboarding},
	{Method: "PUT", Pattern: "/onboarding", Action: "settings.onboarding", TargetType: "settings", Snapshot: ResOnboarding},
	{Method: "PUT", Pattern: "/first-run", Skip: true}, // the Owner's own checklist state, not community data

	{Method: "POST", Pattern: "/emoji", Action: "emoji.create", TargetType: "emoji", Snapshot: ResEmoji, NoBody: true},
	{Method: "PATCH", Pattern: "/emoji/{id}", Action: "emoji.rename", TargetType: "emoji", IDParam: "id", Snapshot: ResEmoji},
	{Method: "DELETE", Pattern: "/emoji/{id}", Action: "emoji.delete", TargetType: "emoji", IDParam: "id", Snapshot: ResEmoji},

	{Method: "POST", Pattern: "/reports/{id}/resolve", Action: "report.resolve", TargetType: "report", IDParam: "id"},
	{Method: "DELETE", Pattern: "/reports/{id}", Action: "report.delete", TargetType: "report", IDParam: "id"},

	{Method: "POST", Pattern: "/automod/rules", Action: "automod.rule_create", TargetType: "automod_rule"},
	{Method: "PUT", Pattern: "/automod/rules/{id}", Action: "automod.rule_update", TargetType: "automod_rule", IDParam: "id"},
	{Method: "DELETE", Pattern: "/automod/rules/{id}", Action: "automod.rule_delete", TargetType: "automod_rule", IDParam: "id"},

	// The text of a moderator note never reaches the log.
	{Method: "POST", Pattern: "/users/{id}/moderation/notes", Action: "moderation.note_add", TargetType: "user", IDParam: "id", NoBody: true},
	{Method: "PUT", Pattern: "/users/{id}/moderation/notes/{noteID}", Action: "moderation.note_edit", TargetType: "user", IDParam: "id", NoBody: true},
	{Method: "DELETE", Pattern: "/users/{id}/moderation/notes/{noteID}", Action: "moderation.note_delete", TargetType: "user", IDParam: "id"},

	{Method: "POST", Pattern: "/media/prune", Action: "media.prune", TargetType: "media"},
	{Method: "POST", Pattern: "/media/orphans/cleanup", Action: "media.orphans_cleanup", TargetType: "media"},
	{Method: "DELETE", Pattern: "/media/{id}", Action: "media.delete", TargetType: "media", IDParam: "id"},

	{Method: "POST", Pattern: "/system/check", Skip: true},
	{Method: "POST", Pattern: "/system/update", Action: "system.update", TargetType: "system"},
	{Method: "POST", Pattern: "/system/mail/test", Action: "system.mail_test", TargetType: "system", NoBody: true},
	{Method: "POST", Pattern: "/status/websocket", Skip: true},
}

// Match finds the route for a request below the admin prefix and its path
// parameters. ok is false for unknown routes and for reads.
func Match(method, path string) (rt Route, params map[string]string, ok bool) {
	rest, found := strings.CutPrefix(path, AdminPrefix)
	if !found {
		return Route{}, nil, false
	}
	segs := split(rest)
	for _, r := range Routes {
		if r.Method != method {
			continue
		}
		if p, m := matchSegs(split(r.Pattern), segs); m {
			return r, p, true
		}
	}
	return Route{}, nil, false
}

func split(p string) []string { return strings.Split(strings.Trim(p, "/"), "/") }

func matchSegs(pat, segs []string) (map[string]string, bool) {
	if len(pat) != len(segs) {
		return nil, false
	}
	params := map[string]string{}
	for i, s := range pat {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			if segs[i] == "" {
				return nil, false
			}
			params[s[1:len(s)-1]] = segs[i]
			continue
		}
		if s != segs[i] {
			return nil, false
		}
	}
	return params, true
}

func mutating(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}
