package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Discord error codes the execute endpoint answers with.
const (
	codeUnknownChannel   = 10003
	codeUnknownWebhook   = 10015
	codeInvalidToken     = 50027
	codeEmptyMessage     = 50006
	codeInvalidFormBody  = 50035
	codeEntityTooLarge   = 40005
	codeUnsupportedMedia = 50035
)

// Execute limits.
const (
	// maxFiles per request (Discord: 10).
	maxFiles = 10
	// maxPayloadJSON is the payload_json part of a multipart request.
	maxPayloadJSON = 1 << 20
	// perWebhookLimit and perChannelLimit follow Discord: about 5 requests per
	// 2 seconds per webhook and 30 per minute per channel.
	perWebhookLimit  = 5
	perWebhookWindow = 2 * time.Second
	perChannelLimit  = 30
	perChannelWindow = time.Minute
	// failedAuthLimit bounds wrong ids and tokens per client address.
	failedAuthLimit  = 30
	failedAuthWindow = time.Minute
)

// DiscordError is the error body of the execute endpoint, in Discord's shape.
type DiscordError struct {
	Message string `json:"message"`
	// Code is Discord's JSON error code (10015 unknown webhook, 50027 invalid
	// token, 50006 empty message, 50035 invalid form body, ...).
	Code int `json:"code"`
}

// DiscordRateLimit is the 429 body of the execute endpoint, in Discord's shape.
type DiscordRateLimit struct {
	Message string `json:"message" enums:"You are being rate limited."`
	// RetryAfter is the number of seconds to wait.
	RetryAfter float64 `json:"retry_after"`
	// Global is always false: limits are per webhook and per channel.
	Global bool `json:"global"`
}

func writeDiscordError(w http.ResponseWriter, status, code int, msg string) {
	httpx.WriteJSON(w, status, DiscordError{Message: msg, Code: code})
}

func writeDiscordRateLimit(w http.ResponseWriter, retry time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	w.Header().Set("X-RateLimit-Scope", "user")
	httpx.WriteJSON(w, http.StatusTooManyRequests, DiscordRateLimit{
		Message: "You are being rate limited.", RetryAfter: retry.Seconds(),
	})
}

// writeFailure maps an error to Discord's shape.
func writeFailure(w http.ResponseWriter, err error) {
	var fe formError
	if errors.As(err, &fe) {
		writeDiscordError(w, http.StatusBadRequest, codeInvalidFormBody, fe.Error())
		return
	}
	switch {
	case errors.As(err, new(emptyMessage)):
		writeDiscordError(w, http.StatusBadRequest, codeEmptyMessage, err.Error())
		return
	case errors.As(err, new(unknownThread)):
		writeDiscordError(w, http.StatusNotFound, codeUnknownChannel, err.Error())
		return
	}
	ae, ok := httpx.AsAPIError(err)
	if !ok {
		ae = httpx.ErrServer(err)
	}
	code := 0
	switch ae.Status {
	case http.StatusBadRequest:
		code = codeInvalidFormBody
	case http.StatusRequestEntityTooLarge:
		code = codeEntityTooLarge
	case http.StatusUnsupportedMediaType:
		code = codeUnsupportedMedia
	}
	writeDiscordError(w, ae.Status, code, ae.Message)
}

// AllowedMentions is Discord's allowed_mentions object.
type AllowedMentions struct {
	// Parse lists the kinds of mentions that notify: "users" and "everyone"
	// (@all and @here). "roles" is accepted and ignored.
	Parse []string `json:"parse"`
	// Users are user IDs (UUIDs) that are notified although "users" is not in
	// Parse.
	Users []string `json:"users"`
	Roles []string `json:"roles"`
}

// ExecutePayload is the body of POST /api/webhooks/{id}/{token}: the subset
// of Discord's execute payload that Mnema Talk uses. Unknown fields (tts,
// components, flags, ...) are accepted and ignored.
type ExecutePayload struct {
	// Content is the message text (Markdown), at most 4000 characters.
	Content string `json:"content" maxLength:"4000"`
	// Username overrides the webhook's name for this message.
	Username string `json:"username" maxLength:"80"`
	// AvatarURL is accepted and ignored: Mnema Talk never loads pictures from
	// other hosts. Set the avatar on the webhook instead.
	AvatarURL string `json:"avatar_url"`
	// Embeds are rich cards (at most 10).
	Embeds []chat.Embed `json:"embeds"`
	// AllowedMentions limits who is notified. Default: members mentioned by
	// @username are, @all and @here are not.
	AllowedMentions *AllowedMentions `json:"allowed_mentions"`
}

// rawPayload is the lenient decoding of ExecutePayload.
type rawPayload struct {
	Content         string           `json:"content"`
	Username        string           `json:"username"`
	Embeds          []inEmbed        `json:"embeds"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions"`
}

// isExecutePath reports whether path is /api/webhooks/{id}/{token}.
func isExecutePath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/webhooks/")
	if !ok {
		return false
	}
	id, token, ok := strings.Cut(rest, "/")
	return ok && id != "" && token != "" && !strings.Contains(token, "/")
}

// IsExecute reports whether r is a webhook execute call. Those carry their
// own secret instead of a cookie, so the same-origin (CSRF) check does not
// apply to them: they are meant for servers and scripts without an Origin.
func IsExecute(r *http.Request) bool {
	return r.Method == http.MethodPost && isExecutePath(r.URL.Path)
}

// authenticate resolves the webhook of the request, counting failures per
// client address.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (*record, bool) {
	fail := func(status, code int, msg string) (*record, bool) {
		if ok, retry := h.failures.Allow(httpx.ClientIPKey(r)); !ok {
			writeDiscordRateLimit(w, retry)
			return nil, false
		}
		writeDiscordError(w, status, code, msg)
		return nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return fail(http.StatusNotFound, codeUnknownWebhook, "Unknown Webhook")
	}
	rec, err := get(r.Context(), h.DB, id)
	if errors.Is(err, errNotFound) {
		return fail(http.StatusNotFound, codeUnknownWebhook, "Unknown Webhook")
	}
	if err != nil {
		writeFailure(w, err)
		return nil, false
	}
	if !rec.tokenMatches(chi.URLParam(r, "token")) {
		return fail(http.StatusUnauthorized, codeInvalidToken, "Invalid Webhook Token")
	}
	return rec, true
}

// execute handles POST /api/webhooks/{id}/{token}.
//
// @Summary Execute a webhook
// @Description Posts a message into the webhook's channel as the webhook (Discord-compatible execute). Authenticated by the secret in the URL only: no cookie or CSRF origin is needed, and cookies are ignored. Send JSON, or multipart/form-data with the JSON in the `payload_json` part and up to 10 files in further parts (same size limit and type checks as member uploads). Errors use Discord's shape ({"message","code"}); 429 carries `retry_after`. Limits: about 5 requests per 2 seconds per webhook and 30 per minute per channel. See doc/webhooks.md for what is supported and ignored.
// @ID executeWebhook
// @Tags Webhooks
// @Accept json,mpfd
// @Produce json
// @Param id path string true "Webhook ID." Format(uuid)
// @Param token path string true "Webhook token."
// @Param wait query bool false "Answer 200 with the created message instead of 204."
// @Param thread_id query string false "ID of a root message of the channel: the post becomes a reply in its thread." Format(uuid)
// @Param payload body ExecutePayload true "Message to post. At least one of content, embeds or files."
// @Success 200 {object} chat.Message "The created message (wait=true)."
// @Success 204 "Posted."
// @Failure 400 {object} DiscordError "Invalid payload (50035) or empty message (50006)."
// @Failure 401 {object} DiscordError "Invalid Webhook Token (50027)."
// @Failure 404 {object} DiscordError "Unknown Webhook (10015) or unknown thread (10003)."
// @Failure 413 {object} DiscordError "Request entity too large (40005)."
// @Failure 429 {object} DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} DiscordError "Server failure."
// @Router /api/webhooks/{id}/{token} [post]
func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	if ok, retry := h.perWebhook.Allow(rec.ID.String()); !ok {
		writeDiscordRateLimit(w, retry)
		return
	}
	if ok, retry := h.perChannel.Allow(rec.ChannelID.String()); !ok {
		writeDiscordRateLimit(w, retry)
		return
	}
	msg, err := h.post(w, r, rec)
	if err != nil {
		writeFailure(w, err)
		return
	}
	h.Events.BroadcastChannel(msg.ChannelID, "message_create", msg)
	if wait, _ := strconv.ParseBool(r.URL.Query().Get("wait")); wait {
		httpx.WriteJSON(w, http.StatusOK, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// request is the parsed body of an execute call.
type request struct {
	payload rawPayload
	files   []*media.StoredFile
}

// readRequest parses the JSON or multipart body. Files are already stored;
// the caller records or discards them.
func (h *Handler) readRequest(w http.ResponseWriter, r *http.Request) (*request, error) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, formError("Content-Type must be application/json or multipart/form-data")
	}
	req := &request{}
	switch mt {
	case "application/json":
		r.Body = http.MaxBytesReader(w, r.Body, httpx.DefaultMaxBody)
		if err := decodeLenient(r.Body, &req.payload); err != nil {
			return nil, h.bodyError(err)
		}
		return req, nil
	case "multipart/form-data":
		r.Body = http.MaxBytesReader(w, r.Body, h.Media.UploadBodyLimit())
		return req, h.readMultipart(r, req)
	}
	return nil, formError("Content-Type must be application/json or multipart/form-data")
}

func decodeLenient(body io.Reader, dst any) error {
	dec := json.NewDecoder(body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

func (h *Handler) bodyError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return httpx.ErrPayloadTooLarge("request body too large")
	}
	return formError("the body is not a valid JSON object")
}

func (h *Handler) readMultipart(r *http.Request, req *request) error {
	mr, err := r.MultipartReader()
	if err != nil {
		return formError("invalid multipart form")
	}
	ctx := r.Context()
	fail := func(err error) error {
		for _, f := range req.files {
			h.Media.DiscardFile(ctx, f)
		}
		req.files = nil
		return err
	}
	form := map[string]string{}
	var jsonPart bool
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(h.Media.FormError(err))
		}
		switch {
		case part.FileName() != "":
			if len(req.files) >= maxFiles {
				return fail(formError("at most 10 files are allowed"))
			}
			f, err := h.Media.StoreFilePart(ctx, part)
			if err != nil {
				return fail(err)
			}
			req.files = append(req.files, f)
		case part.FormName() == "payload_json":
			b, err := io.ReadAll(io.LimitReader(part, maxPayloadJSON+1))
			if err != nil {
				return fail(h.Media.FormError(err))
			}
			if len(b) > maxPayloadJSON {
				return fail(httpx.ErrPayloadTooLarge("payload_json too large"))
			}
			if err := decodeLenient(strings.NewReader(string(b)), &req.payload); err != nil {
				return fail(formError("payload_json is not a valid JSON object"))
			}
			jsonPart = true
		case part.FormName() == "content" || part.FormName() == "username":
			b, err := io.ReadAll(io.LimitReader(part, chat.MaxMessageLen*4+1))
			if err != nil {
				return fail(h.Media.FormError(err))
			}
			form[part.FormName()] = string(b)
		}
	}
	// Plain form fields stand in for a missing payload_json, as at Discord.
	if !jsonPart {
		req.payload.Content = form["content"]
		req.payload.Username = form["username"]
	}
	return nil
}

// post validates and stores the message. It returns the stored message.
func (h *Handler) post(w http.ResponseWriter, r *http.Request, rec *record) (*chat.Message, error) {
	ctx := r.Context()
	req, err := h.readRequest(w, r)
	if err != nil {
		return nil, err
	}
	discard := func() {
		for _, f := range req.files {
			h.Media.DiscardFile(ctx, f)
		}
	}
	nm, mentions, err := h.build(ctx, r, rec, req)
	if err != nil {
		discard()
		return nil, err
	}
	ctx = chat.WithMentionPolicy(ctx, mentions)
	id, err := chat.InsertMessage(ctx, h.DB, h.Online, *nm, func(tx pgx.Tx, msgID uuid.UUID) error {
		for _, f := range req.files {
			if err := h.Media.RecordFile(ctx, tx, f, rec.botUserID, rec.ChannelID, msgID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		discard()
		return nil, err
	}
	return chat.GetMessage(ctx, h.DB, id)
}

// build validates a parsed request into a message and the mention policy.
func (h *Handler) build(ctx context.Context, r *http.Request, rec *record, req *request) (*chat.NewMessage, chat.MentionPolicy, error) {
	p := req.payload
	content, err := chat.ValidateContent(p.Content, true)
	if err != nil {
		return nil, chat.MentionPolicy{}, formError(err.Error())
	}
	name := rec.Name
	if strings.TrimSpace(p.Username) != "" {
		if name, err = cleanName("username", p.Username); err != nil {
			return nil, chat.MentionPolicy{}, formError(err.Error())
		}
	}
	embeds, err := sanitizeEmbeds(p.Embeds)
	if err != nil {
		return nil, chat.MentionPolicy{}, err
	}
	if content == "" && len(embeds) == 0 && len(req.files) == 0 {
		return nil, chat.MentionPolicy{}, emptyMessage{}
	}
	var parent *uuid.UUID
	if raw := r.URL.Query().Get("thread_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, chat.MentionPolicy{}, unknownThread{}
		}
		if err := chat.ValidateParent(ctx, h.DB, rec.ChannelID, &id); err != nil {
			return nil, chat.MentionPolicy{}, unknownThread{}
		}
		parent = &id
	}
	policy, err := h.mentionPolicy(ctx, rec, p.AllowedMentions)
	if err != nil {
		return nil, chat.MentionPolicy{}, err
	}
	return &chat.NewMessage{
		ChannelID: rec.ChannelID, UserID: rec.botUserID, Content: content, ParentID: parent,
		WebhookID: &rec.ID, AuthorName: name, Embeds: embeds,
	}, policy, nil
}

// emptyMessage and unknownThread are executed-call errors with their own
// Discord codes.
type emptyMessage struct{}

func (emptyMessage) Error() string { return "Cannot send an empty message" }

type unknownThread struct{}

func (unknownThread) Error() string { return "Unknown Channel" }

// mentionPolicy turns allowed_mentions into the policy for the message. The
// default notifies mentioned members but never @all and @here. Those need
// "everyone" in parse and the webhook's creator to hold Mention everyone in
// the channel right now: a webhook cannot give anyone more power than its
// creator has.
func (h *Handler) mentionPolicy(ctx context.Context, rec *record, am *AllowedMentions) (chat.MentionPolicy, error) {
	policy := chat.MentionPolicy{NoEveryone: true}
	if am != nil {
		wantUsers := slices.Contains(am.Parse, "users")
		wantEveryone := slices.Contains(am.Parse, "everyone")
		if wantUsers && len(am.Users) > 0 {
			return policy, formError("allowed_mentions: users cannot be in parse and listed at once")
		}
		if !wantUsers {
			policy.NoUsers = true
			for _, raw := range am.Users {
				if id, err := uuid.Parse(raw); err == nil {
					policy.AllowUsers = append(policy.AllowUsers, id)
				}
			}
		}
		policy.NoEveryone = !wantEveryone
	}
	if h.Perms == nil {
		return policy, nil
	}
	snap, err := h.Perms.ForChannel(ctx, rec.ChannelID)
	if err != nil {
		return policy, err
	}
	if !policy.NoEveryone {
		policy.NoEveryone = rec.CreatedBy == nil || !snap.Channel(perm.Subject{ID: *rec.CreatedBy}, rec.ChannelID).Has(perm.MentionEveryone)
	}
	ch := rec.ChannelID
	policy.CanView = func(id uuid.UUID) bool { return snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) }
	return policy, nil
}
