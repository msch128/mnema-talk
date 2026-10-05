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
// @license.name	MIT
// @license.url	https://opensource.org/license/mit
// @BasePath		/

// @securityDefinitions.apikey	cookieAuth
// @in							cookie
// @name						mnema_session
// @description				HttpOnly session cookie issued by POST /api/auth/login or /api/auth/register. The name is __Host-mnema_session when PUBLIC_URL is https.

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

// @tag.name			Admin
// @tag.description	Administrator-only management.
