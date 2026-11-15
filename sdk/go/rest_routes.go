package mnema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

// MessageQuery pages through a channel's messages: at most one of Before,
// After and Around (message ids); Limit is 1 to 100 (0 means the server's
// default of 50).
type MessageQuery struct {
	Limit  int
	Before string
	After  string
	Around string
}

func (q MessageQuery) values() url.Values {
	v := url.Values{}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	for k, x := range map[string]string{"before": q.Before, "after": q.After, "around": q.Around} {
		if x != "" {
			v.Set(k, x)
		}
	}
	return v
}

// MessageCreate is the body of a new message. A message needs content,
// embeds or components.
type MessageCreate struct {
	Content          string            `json:"content,omitempty"`
	Embeds           []Embed           `json:"embeds,omitempty"`
	Components       []Component       `json:"components,omitempty"`
	AllowedMentions  *AllowedMentions  `json:"allowed_mentions,omitempty"`
	MessageReference *MessageReference `json:"message_reference,omitempty"`
	// ParentID makes the message a reply in the thread of that message.
	ParentID string `json:"parent_id,omitempty"`
	// LifetimeSeconds makes the server delete the message that long after it
	// is stored, as for a member: 60 to 2592000 and at most the community's
	// ceiling (doc/timed-messages.md). 0 sends none. Refused with 400 when
	// lifetimes are off or out of bounds.
	LifetimeSeconds int `json:"lifetime_seconds,omitempty"`
}

// MessageEdit changes a bot's own message. Nil fields stay as they are;
// Components set to an empty slice removes the components.
type MessageEdit struct {
	Content    *string      `json:"content,omitempty"`
	Components *[]Component `json:"components,omitempty"`
	// AllowedMentions is whom the new text notifies, as on a post; an edit
	// with it counts as a post for the channel's limit.
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

// Upload is a file to post. Reader is read completely before the request (so
// a rate-limited upload can be sent again). Content is an optional caption.
type Upload struct {
	Name            string
	Reader          io.Reader
	Content         string
	AllowedMentions *AllowedMentions
	ParentID        string
	ReplyToID       string
}

// Command is a registered slash command.
type Command struct {
	ID          string          `json:"id"`
	BotID       string          `json:"bot_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Options     []CommandOption `json:"options"`
	Disabled    bool            `json:"disabled"`
}

// CommandInput is a command to register.
type CommandInput struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Options     []CommandOption `json:"options"`
}

// OptionType is the type of a command option.
type OptionType int

// The option types.
const (
	OptionString  OptionType = 3
	OptionInteger OptionType = 4
	OptionBoolean OptionType = 5
	OptionUser    OptionType = 6
	OptionChannel OptionType = 7
)

// CommandOption is one option of a command. Required options come first;
// Choices (text options only) restrict the values.
type CommandOption struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Type        OptionType `json:"type"`
	Required    bool       `json:"required"`
	Choices     []string   `json:"choices,omitempty"`
}

// GatewayInfo is the answer of GET /gateway.
type GatewayInfo struct {
	URL               string `json:"url"`
	HeartbeatInterval int    `json:"heartbeat_interval"`
	MaxConnections    int    `json:"max_connections"`
}

func esc(s string) string { return url.PathEscape(s) }

// Me returns the bot account.
func (r *REST) Me(ctx context.Context) (*Self, error) {
	return call[Self](ctx, r, http.MethodGet, "/users/@me", nil)
}

// Gateway returns the gateway's address.
func (r *REST) Gateway(ctx context.Context) (*GatewayInfo, error) {
	return call[GatewayInfo](ctx, r, http.MethodGet, "/gateway", nil)
}

// Channels returns the channels the bot can view, with their categories.
func (r *REST) Channels(ctx context.Context) (*ChannelHierarchy, error) {
	return call[ChannelHierarchy](ctx, r, http.MethodGet, "/channels", nil)
}

// Messages returns a page of a channel's messages.
func (r *REST) Messages(ctx context.Context, channelID string, q MessageQuery) ([]Message, error) {
	var out []Message
	_, err := r.do(ctx, request{method: http.MethodGet, path: "/channels/" + esc(channelID) + "/messages", query: q.values(), out: &out})
	return out, err
}

// Message returns one message.
func (r *REST) Message(ctx context.Context, channelID, messageID string) (*Message, error) {
	return call[Message](ctx, r, http.MethodGet, "/channels/"+esc(channelID)+"/messages/"+esc(messageID), nil)
}

// CreateMessage posts a message. Components are validated first.
func (r *REST) CreateMessage(ctx context.Context, channelID string, m MessageCreate) (*Message, error) {
	if err := ValidateComponents(m.Components); err != nil {
		return nil, err
	}
	return call[Message](ctx, r, http.MethodPost, "/channels/"+esc(channelID)+"/messages", m)
}

// EditMessage changes one of the bot's own messages.
func (r *REST) EditMessage(ctx context.Context, channelID, messageID string, e MessageEdit) (*Message, error) {
	if e.Components != nil {
		if err := ValidateComponents(*e.Components); err != nil {
			return nil, err
		}
	}
	return call[Message](ctx, r, http.MethodPatch, "/channels/"+esc(channelID)+"/messages/"+esc(messageID), e)
}

// DeleteMessage deletes a message (the bot's own, or others' with Manage
// messages).
func (r *REST) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	_, err := r.do(ctx, request{method: http.MethodDelete, path: "/channels/" + esc(channelID) + "/messages/" + esc(messageID)})
	return err
}

func reactionPath(channelID, messageID, emoji string) string {
	return "/channels/" + esc(channelID) + "/messages/" + esc(messageID) + "/reactions/" + esc(emoji) + "/@me"
}

// AddReaction reacts to a message with a Unicode emoji or a custom emoji
// token (<:name:id>).
func (r *REST) AddReaction(ctx context.Context, channelID, messageID, emoji string) error {
	_, err := r.do(ctx, request{method: http.MethodPut, path: reactionPath(channelID, messageID, emoji)})
	return err
}

// RemoveReaction takes the bot's reaction back.
func (r *REST) RemoveReaction(ctx context.Context, channelID, messageID, emoji string) error {
	_, err := r.do(ctx, request{method: http.MethodDelete, path: reactionPath(channelID, messageID, emoji)})
	return err
}

// Upload posts a file, with an optional caption.
func (r *REST) Upload(ctx context.Context, channelID string, u Upload) (*Message, error) {
	body, ctype, err := u.multipart()
	if err != nil {
		return nil, err
	}
	out := new(Message)
	empty, err := r.do(ctx, request{method: http.MethodPost, path: "/channels/" + esc(channelID) + "/upload", body: body, contentType: ctype, out: out})
	if err != nil || empty {
		return nil, err
	}
	return out, nil
}

func (u Upload) multipart() ([]byte, string, error) {
	if u.Reader == nil || u.Name == "" {
		return nil, "", errors.New("mnema: an upload needs a Name and a Reader")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fields := map[string]string{"content": u.Content, "parent_id": u.ParentID, "reply_to_id": u.ReplyToID}
	if u.AllowedMentions != nil {
		b, err := json.Marshal(u.AllowedMentions)
		if err != nil {
			return nil, "", fmt.Errorf("mnema: encode allowed_mentions: %w", err)
		}
		fields["allowed_mentions"] = string(b)
	}
	for k, v := range fields {
		if v == "" {
			continue
		}
		if err := w.WriteField(k, v); err != nil {
			return nil, "", fmt.Errorf("mnema: build upload: %w", err)
		}
	}
	part, err := w.CreateFormFile("file", u.Name)
	if err == nil {
		_, err = io.Copy(part, u.Reader)
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		return nil, "", fmt.Errorf("mnema: build upload: %w", err)
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// Members lists the members who share a channel with the bot (needs the
// GUILD_MEMBERS intent allowed).
func (r *REST) Members(ctx context.Context) ([]Member, error) {
	var out []Member
	_, err := r.do(ctx, request{method: http.MethodGet, path: "/members", out: &out})
	return out, err
}

// Member returns one member.
func (r *REST) Member(ctx context.Context, userID string) (*Member, error) {
	return call[Member](ctx, r, http.MethodGet, "/members/"+esc(userID), nil)
}

// Commands lists the bot's commands.
func (r *REST) Commands(ctx context.Context) ([]Command, error) {
	var out []Command
	_, err := r.do(ctx, request{method: http.MethodGet, path: "/commands", out: &out})
	return out, err
}

// SetCommands replaces all of the bot's commands (Discord's bulk overwrite).
func (r *REST) SetCommands(ctx context.Context, cmds []CommandInput) ([]Command, error) {
	list := make([]CommandInput, 0, len(cmds))
	for _, c := range cmds {
		if c.Options == nil {
			c.Options = []CommandOption{}
		}
		list = append(list, c)
	}
	cmds = list
	var out []Command
	req, err := jsonRequest(http.MethodPut, "/commands", cmds, &out)
	if err == nil {
		_, err = r.do(ctx, req)
	}
	return out, err
}

// CreateCommand adds a command, or updates the one of the same name.
func (r *REST) CreateCommand(ctx context.Context, cmd CommandInput) (*Command, error) {
	if cmd.Options == nil {
		cmd.Options = []CommandOption{}
	}
	return call[Command](ctx, r, http.MethodPost, "/commands", cmd)
}

// DeleteCommand removes a command.
func (r *REST) DeleteCommand(ctx context.Context, commandID string) error {
	_, err := r.do(ctx, request{method: http.MethodDelete, path: "/commands/" + esc(commandID)})
	return err
}
