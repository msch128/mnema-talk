package bot

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	// tokenLen is the length of every token this server issues.
	tokenLen = len(TokenPrefix) + 43
	// touchEvery bounds how often a token's last-used time is written.
	touchEvery = time.Minute
	// touchTimeout bounds the last-used write on the request path.
	touchTimeout = 3 * time.Second

	// Per-bot limits, in Discord's spirit: a global budget and a tighter one
	// for sending messages in one channel.
	globalLimit  = 50
	globalWindow = time.Second
	sendLimit    = 5
	sendWindow   = 5 * time.Second
	// failedLimit bounds wrong tokens per client address.
	failedLimit  = 30
	failedWindow = time.Minute
)

var errUnauthorized = httpx.ErrUnauthorized("401: Unauthorized")

// Authenticator admits requests that carry a valid bot token and applies the
// per-bot rate limits. It reads only the Authorization header: cookies are
// never looked at, so the routes behind it have nothing to forge.
type Authenticator struct {
	DB *db.Pool
	// Perms re-checks on use that the person who issued a token may still
	// act as its bot; nil skips the check (tests).
	Perms    *perm.Service
	failures *httpx.RateLimiter
	global   *httpx.RateLimiter
	sends    *httpx.RateLimiter
	now      func() time.Time

	mu      sync.Mutex
	touched map[uuid.UUID]time.Time
	minters map[uuid.UUID]minterVerdict
	// bindTried remembers when binding a legacy token was last attempted.
	bindTried map[uuid.UUID]time.Time
}

// NewAuthenticator returns an Authenticator with its rate limiters.
func NewAuthenticator(p *db.Pool) *Authenticator {
	return &Authenticator{
		DB:       p,
		failures: httpx.NewRateLimiter(failedLimit, failedWindow).Named("bot_auth"),
		global:   httpx.NewRateLimiter(globalLimit, globalWindow).Named("bot_api"),
		sends:    httpx.NewRateLimiter(sendLimit, sendWindow).Named("bot_send"),
		now:      time.Now,
		touched:  map[uuid.UUID]time.Time{},
	}
}

// SetLimits replaces the global (per second) and send (per 5 seconds) limits.
// For tests.
func (a *Authenticator) SetLimits(global, send int) {
	a.global = httpx.NewRateLimiter(global, globalWindow).Named("bot_api")
	a.sends = httpx.NewRateLimiter(send, sendWindow).Named("bot_send")
}

// Credentials is what a valid token resolves to.
type Credentials struct {
	Bot  auth.BotInfo
	User *auth.User
}

// Lookup resolves a token. Unknown, expired and revoked tokens and disabled
// bots all give errUnauthorized, so nothing tells them apart.
func (a *Authenticator) Lookup(ctx context.Context, token string) (*Credentials, error) {
	if len(token) != tokenLen || !strings.HasPrefix(token, TokenPrefix) {
		return nil, errUnauthorized
	}
	hash := HashToken(token)
	var c Credentials
	var userID uuid.UUID
	var lastUsed *time.Time
	var minter *uuid.UUID
	err := a.DB.QueryRow(ctx, `
		SELECT t.id, b.id, b.user_id, b.name, b.allowed_intents, t.last_used_at, t.created_by
		FROM bot_tokens t JOIN bots b ON b.id = t.bot_id JOIN users u ON u.id = b.user_id
		WHERE t.token_hash = $1 AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > NOW()) AND NOT b.disabled
		  AND `+auth.ActiveSQL("u"),
		hash).Scan(&c.Bot.TokenID, &c.Bot.BotID, &userID, &c.Bot.Name, &c.Bot.Intents, &lastUsed, &minter)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if err := a.CheckMinter(ctx, c.Bot.TokenID, minter, userID); err != nil {
		return nil, err
	}
	// The account exists as long as the bot does (the foreign key cascades).
	if c.User, err = auth.GetUser(ctx, a.DB, userID); err != nil {
		return nil, err
	}
	a.touch(ctx, &c.Bot, lastUsed)
	return &c, nil
}

// shouldTouch reports whether a token should have its last-used timestamp refreshed.
func (a *Authenticator) shouldTouch(id uuid.UUID, last *time.Time, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if prev, seen := a.touched[id]; seen {
		if now.Sub(prev) < touchEvery {
			return false
		}
		a.touched[id] = now
		return true
	}
	if last != nil && now.Sub(*last) < touchEvery {
		return false
	}
	if len(a.touched) > 4096 {
		clear(a.touched)
	}
	a.touched[id] = now
	return true
}

func (a *Authenticator) recordTouch(ctx context.Context, tokenID, botID uuid.UUID) {
	// Detached from the request so a disconnect does not lose the write, but
	// bounded so a slow database cannot hold the request for long.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), touchTimeout)
	defer cancel()
	if _, err := a.DB.Exec(ctx, `
		WITH t AS (UPDATE bot_tokens SET last_used_at = NOW() WHERE id = $1)
		UPDATE bots SET last_active_at = NOW() WHERE id = $2`, tokenID, botID); err != nil {
		slog.Warn("bot token last-used", "err", err)
	}
}

// touch records the use of a token and of its bot, at most once a minute per
// token. A failure only loses a timestamp.
func (a *Authenticator) touch(ctx context.Context, b *auth.BotInfo, last *time.Time) {
	if !a.shouldTouch(b.TokenID, last, a.now()) {
		return
	}
	a.recordTouch(ctx, b.TokenID, b.BotID)
}

// BearerToken reads the credential of an Authorization header: "Bot <token>".
func BearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || subtle.ConstantTimeCompare([]byte(strings.ToLower(scheme)), []byte("bot")) != 1 {
		return "", false
	}
	return strings.TrimSpace(token), true
}

// Middleware requires a bot token, puts the bot's account and the bot into
// the context and applies the per-bot global rate limit. Errors are in
// Discord's shape.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := httpx.ClientIPKey(r)
		token, ok := BearerToken(r)
		var c *Credentials
		var err error
		if ok {
			c, err = a.Lookup(r.Context(), token)
		} else {
			err = errUnauthorized
		}
		if err != nil {
			if apiErr, isAPI := httpx.AsAPIError(err); isAPI && apiErr.Status == http.StatusUnauthorized {
				if allowed, retry := a.failures.Allow(ip); !allowed {
					httpx.WriteDiscordRateLimit(w, retry, true)
					return
				}
			}
			httpx.WriteDiscordError(w, err)
			return
		}
		if allowed, retry := a.global.Allow(c.Bot.BotID.String()); !allowed {
			httpx.WriteDiscordRateLimit(w, retry, true)
			return
		}
		ctx := auth.WithBot(auth.WithUser(r.Context(), c.User), &c.Bot)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LimitSends applies the send limit per bot and channel (the channelID path
// parameter) to the routes it wraps.
func (a *Authenticator) LimitSends(channelParam func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A parameter that is no UUID is refused by the handler.
			if b, ch := auth.BotFrom(r.Context()), channelParam(r); b != nil && ch != "" {
				if allowed, retry := a.sends.Allow(b.BotID.String() + ":" + ch); !allowed {
					httpx.WriteDiscordRateLimit(w, retry, false)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
