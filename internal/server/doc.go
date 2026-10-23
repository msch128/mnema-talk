package server

// General OpenAPI information. api/openapi.json is generated from these
// annotations and from the handler annotations in internal/*; run
// `make openapi` after changing any of them (`make openapi-check` fails when
// the file is stale).

// @title			Mnema Talk API
// @version		1.0.0
// @description	REST API of Mnema Talk, a self-hosted, invite-only chat and voice app for a single community.
// @description	Authentication is an HttpOnly session cookie set by login/register. State-changing requests (anything but GET/HEAD/OPTIONS) must carry a same-origin Origin header, otherwise 403.
// @description	JSON bodies are decoded strictly: unknown fields are rejected with 400 and bodies are limited to 1 MiB. Errors share the ErrorResponse shape.
// @description	Everything except the SPA assets lives under /api.
// @description	Conventions. Creating returns 201 with the new object; a delete or a change without a result returns 204 with no body, while a few operations return 200 with the changed object (for example timing out a member or unpinning a message), as their descriptions say.
// @description	Every 429 carries a Retry-After header (seconds). Lists that page do so with `limit` and, depending on the list, `before` / `after` (an id) or a cursor; each operation names its parameters and maximum.
// @description	Uploads are the one exception to the 1 MiB body limit; their size limits are on the operations.
// @description	A 403 names its cause in the message: a missing permission (named on the operation), a role that is not below yours, or a cross-origin request refused by the Origin / Sec-Fetch-Site check.
// @description	A 403 with the code RULES_NOT_ACCEPTED means the member has not accepted the community rules yet. It is returned by every operation that posts, reacts, uploads or joins a Talk (sending messages and polls, voting, reactions, uploads, slash commands); accept the rules with POST /api/onboarding/accept first.
// @license.name	AGPL-3.0-only
// @license.url	https://www.gnu.org/licenses/agpl-3.0.html
// @BasePath		/

// @securityDefinitions.apikey	cookieAuth
// @in							cookie
// @name						mnema_session
// @description				HttpOnly session cookie issued by POST /api/auth/login or /api/auth/register. The name is __Host-mnema_session when PUBLIC_URL is https.

// @securityDefinitions.apikey	botAuth
// @in							header
// @name						Authorization
// @description				A bot token, sent as `Authorization: Bot <token>`. Only for /api/bot/v1.

// @securitydefinitions.bearerauth	bearerAuth
// @description					METRICS_TOKEN, only for /api/metrics.

// @tag.name			System
// @tag.description	Health, metrics and public information.

// @tag.name			Auth
// @tag.description	Sessions and credentials.

// @tag.name			Users
// @tag.description	Profiles, presence and members.

// @tag.name			Channels
// @tag.description	Channel hierarchy and read state.

// @tag.name			Messages
// @tag.description	Messages, threads, reactions and search.

// @tag.name			Media
// @tag.description	Uploads, downloads and link previews.

// @tag.name			Realtime
// @tag.description	WebSocket and WebRTC configuration.

// @tag.name			Commands
// @tag.description	Slash commands of bots: listing them in a channel and running them.

// @tag.name			Bot API
// @tag.description	The REST API of bot accounts (/api/bot/v1), authenticated with a bot token. Errors use Discord's shape; see doc/bots.md for the gateway.

// @tag.name			Bots
// @tag.description	Managing bot accounts, roles and tokens (Owner or Manage bots).

// @tag.name			Polls
// @tag.description	Polls in a channel: create, vote, end.

// @tag.name			Push
// @tag.description	Web Push: the server's public key, a device's subscriptions, preferences and a test notification.

// @tag.name			Emoji
// @tag.description	Custom emoji of the community: list, images and (with Manage emoji) upload, rename and delete.

// @tag.name			Roles
// @tag.description	Roles, member roles, permission lists and channel or category overwrites (Manage roles or Manage channels).

// @tag.name			Webhooks
// @tag.description	Incoming and outgoing webhooks of a channel (Owner and Administrators only).

// @tag.name			Onboarding
// @tag.description	The community's name, welcome text and rules, and a member's acceptance of them.

// @tag.name			Admin
// @tag.description	Management endpoints. Each operation names the permission it needs: Owner only, a permission such as Manage members, Ban members or Manage channels, or the Administrator permission.
