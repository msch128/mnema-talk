package mnema

import (
	"context"
	"errors"
	"net/http"
)

// Callback types of an interaction response.
const (
	callbackMessage        = 4
	callbackDeferredReply  = 5
	callbackDeferredUpdate = 6
	callbackUpdate         = 7
	callbackModal          = 9
)

// FlagEphemeral shows a response to the member who caused the interaction
// only; it is never stored.
const FlagEphemeral = 64

// Response is a message sent in answer to an interaction (Reply, FollowUp).
type Response struct {
	Content    string
	Embeds     []Embed
	Components []Component
	// Ephemeral shows the response to the member only (flags 64).
	Ephemeral bool
	// AllowedMentions narrows whom the message notifies, as on a post; nil
	// notifies the members it names, never @all, @here or a role.
	AllowedMentions *AllowedMentions
}

// Edit changes a response or the message a component is on (EditReply,
// Update). Nil fields stay as they are; an empty Components slice removes
// the components. It has no Ephemeral: the first response (Reply or
// DeferReply) decides whether an answer is ephemeral, and the server refuses
// flags on an edit.
type Edit struct {
	Content    *string      `json:"content,omitempty"`
	Embeds     *[]Embed     `json:"embeds,omitempty"`
	Components *[]Component `json:"components,omitempty"`
	// AllowedMentions narrows whom the new text notifies, as on a post.
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

type responseData struct {
	Content    string      `json:"content"`
	Embeds     []Embed     `json:"embeds,omitempty"`
	Components []Component `json:"components,omitempty"`
	Flags      int         `json:"flags"`
	// AllowedMentions is sent only when set.
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

func (r Response) data() (responseData, error) {
	if err := ValidateComponents(r.Components); err != nil {
		return responseData{}, err
	}
	d := responseData{Content: r.Content, Embeds: r.Embeds, Components: r.Components, AllowedMentions: r.AllowedMentions}
	if r.Ephemeral {
		d.Flags = FlagEphemeral
	}
	return d, nil
}

func (e Edit) validate() error {
	if e.Components == nil {
		return nil
	}
	return ValidateComponents(*e.Components)
}

// ErrAlreadyAnswered is returned when an interaction gets a second first
// answer; use EditReply or FollowUp instead.
var ErrAlreadyAnswered = errors.New("mnema: this interaction was already answered; use EditReply or FollowUp")

// ErrNotFromComponent is returned by Update and DeferUpdate on an
// interaction that did not come from a component.
var ErrNotFromComponent = errors.New("mnema: only for a component interaction or a modal submit opened from one")

func (i *Interaction) path(suffix string) string {
	return "/interactions/" + esc(i.ID) + "/" + esc(i.Token) + suffix
}

func (i *Interaction) client() (*REST, error) {
	if i.rest == nil {
		return nil, errors.New("mnema: the interaction has no REST client (it was not delivered by a Client)")
	}
	return i.rest, nil
}

// callback sends the first answer; it may only succeed once.
func (i *Interaction) callback(ctx context.Context, typ int, data any) error {
	r, err := i.client()
	if err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.answered {
		return ErrAlreadyAnswered
	}
	body := map[string]any{"type": typ}
	if data != nil {
		body["data"] = data
	}
	req, err := jsonRequest(http.MethodPost, i.path("/callback"), body, nil)
	if err == nil {
		_, err = r.do(ctx, req)
	}
	if err == nil {
		i.answered = true
	}
	return err
}

// Reply posts the response message (callback 4).
func (i *Interaction) Reply(ctx context.Context, r Response) error {
	d, err := r.data()
	if err != nil {
		return err
	}
	return i.callback(ctx, callbackMessage, d)
}

// DeferReply acknowledges now (callback 5); the answer follows with EditReply
// or FollowUp. With ephemeral the answer is shown to the member only.
func (i *Interaction) DeferReply(ctx context.Context, ephemeral bool) error {
	var data any
	if ephemeral {
		data = map[string]int{"flags": FlagEphemeral}
	}
	return i.callback(ctx, callbackDeferredReply, data)
}

// EditReply edits the original response, or posts it after DeferReply. The
// message is nil for an ephemeral note (the server answers 204).
func (i *Interaction) EditReply(ctx context.Context, e Edit) (*Message, error) {
	r, err := i.client()
	if err != nil {
		return nil, err
	}
	if err := e.validate(); err != nil {
		return nil, err
	}
	return call[Message](ctx, r, http.MethodPatch, i.path("/messages/@original"), e)
}

// DeleteReply deletes the original response.
func (i *Interaction) DeleteReply(ctx context.Context) error {
	r, err := i.client()
	if err != nil {
		return err
	}
	_, err = r.do(ctx, request{method: http.MethodDelete, path: i.path("/messages/@original")})
	return err
}

// FollowUp sends a further message (up to 5 responses in total). The message
// is nil for an ephemeral one.
func (i *Interaction) FollowUp(ctx context.Context, resp Response) (*Message, error) {
	r, err := i.client()
	if err != nil {
		return nil, err
	}
	d, err := resp.data()
	if err != nil {
		return nil, err
	}
	return call[Message](ctx, r, http.MethodPost, i.path("/followups"), d)
}

func (i *Interaction) fromComponent() bool {
	return i.IsComponent() || (i.IsModalSubmit() && i.Message != nil)
}

// Update changes the message the component is on (callback 7).
func (i *Interaction) Update(ctx context.Context, e Edit) error {
	if !i.fromComponent() {
		return ErrNotFromComponent
	}
	if err := e.validate(); err != nil {
		return err
	}
	return i.callback(ctx, callbackUpdate, e)
}

// DeferUpdate acknowledges a click without changing anything yet (callback
// 6).
func (i *Interaction) DeferUpdate(ctx context.Context) error {
	if !i.fromComponent() {
		return ErrNotFromComponent
	}
	return i.callback(ctx, callbackDeferredUpdate, nil)
}

// ShowModal opens a modal (callback 9); a modal submit cannot open another.
func (i *Interaction) ShowModal(ctx context.Context, m Modal) error {
	if i.IsModalSubmit() {
		return errors.New("mnema: a modal submit cannot open another modal")
	}
	if err := m.Validate(); err != nil {
		return err
	}
	return i.callback(ctx, callbackModal, m)
}
