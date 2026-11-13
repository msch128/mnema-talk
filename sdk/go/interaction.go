package mnema

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
)

// InteractionType is the type of an interaction.
type InteractionType int

// The interaction types.
const (
	InteractionCommand     InteractionType = 2
	InteractionComponent   InteractionType = 3
	InteractionModalSubmit InteractionType = 5
)

// Interaction is an INTERACTION_CREATE dispatch: a member ran a command
// (type 2), used a component (type 3) or submitted a modal (type 5). Answer
// it within 15 minutes with Reply, DeferReply, Update, DeferUpdate or
// ShowModal; then EditReply, DeleteReply and FollowUp work for the same 15
// minutes.
type Interaction struct {
	ID            string             `json:"id"`
	Token         string             `json:"token"`
	Type          InteractionType    `json:"type"`
	ChannelID     string             `json:"channel_id"`
	ApplicationID string             `json:"application_id"`
	Member        *InteractionMember `json:"member,omitempty"`
	// User is the member who caused the interaction (Member.User). The server
	// sends it with every interaction; one without it is not delivered.
	User User `json:"-"`
	// Message is the message the component is on (type 3, and type 5 when
	// the modal came from a component).
	Message *Message        `json:"message,omitempty"`
	Data    InteractionData `json:"data"`

	rest     *REST
	mu       sync.Mutex
	answered bool
}

// InteractionMember wraps the member of an interaction, as Discord does.
type InteractionMember struct {
	User User `json:"user"`
}

// InteractionData is the data of an interaction; which fields are set
// depends on the type.
type InteractionData struct {
	// ID and Name identify the command (type 2); Options are its values.
	ID      string              `json:"id,omitempty"`
	Name    string              `json:"name,omitempty"`
	Options []InteractionOption `json:"options,omitempty"`
	// CustomID is the component's (type 3) or the modal's (type 5).
	CustomID      string        `json:"custom_id,omitempty"`
	ComponentType ComponentType `json:"component_type,omitempty"`
	// Values are the picked values of a select.
	Values []string `json:"values,omitempty"`
	// Resolved describes the picks of user, role and channel selects.
	Resolved *Resolved `json:"resolved,omitempty"`
	// Components are the submitted rows of a modal (type 5).
	Components []Component `json:"components,omitempty"`
}

// Resolved holds what user, role and channel selects picked, keyed by id.
type Resolved struct {
	Users    map[string]User            `json:"users,omitempty"`
	Roles    map[string]ResolvedRole    `json:"roles,omitempty"`
	Channels map[string]ResolvedChannel `json:"channels,omitempty"`
}

// ResolvedRole is a role picked in a role select.
type ResolvedRole struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color int    `json:"color"`
}

// ResolvedChannel is a channel picked in a channel select.
type ResolvedChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// InteractionOption is one option value of a command.
type InteractionOption struct {
	Name  string          `json:"name"`
	Type  OptionType      `json:"type"`
	Value json.RawMessage `json:"value"`
}

// String returns a text, user or channel option ("" when missing or of
// another type). It is safe on a nil option.
func (o *InteractionOption) String() string {
	var s string
	if o != nil && json.Unmarshal(o.Value, &s) == nil {
		return s
	}
	return ""
}

// Int returns an integer option (0 when missing or of another type).
func (o *InteractionOption) Int() int64 {
	if o == nil {
		return 0
	}
	d := json.NewDecoder(bytes.NewReader(o.Value))
	d.UseNumber()
	var n json.Number
	if d.Decode(&n) != nil {
		return 0
	}
	if i, err := n.Int64(); err == nil {
		return i
	}
	f, _ := strconv.ParseFloat(n.String(), 64)
	return int64(f)
}

// Bool returns a boolean option (false when missing or of another type).
func (o *InteractionOption) Bool() bool {
	var b bool
	return o != nil && json.Unmarshal(o.Value, &b) == nil && b
}

var errNoInteractionUser = errors.New("mnema: an interaction without member.user")

// UnmarshalJSON decodes the interaction and fills User from Member.
func (i *Interaction) UnmarshalJSON(b []byte) error {
	type plain struct {
		ID            string             `json:"id"`
		Token         string             `json:"token"`
		Type          InteractionType    `json:"type"`
		ChannelID     string             `json:"channel_id"`
		ApplicationID string             `json:"application_id"`
		Member        *InteractionMember `json:"member"`
		User          *User              `json:"user"`
		Message       *Message           `json:"message"`
		Data          InteractionData    `json:"data"`
	}
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	i.ID, i.Token, i.Type, i.ChannelID, i.ApplicationID = p.ID, p.Token, p.Type, p.ChannelID, p.ApplicationID
	i.Member, i.Message, i.Data = p.Member, p.Message, p.Data
	switch {
	case p.User != nil:
		i.User = *p.User
	case p.Member != nil:
		i.User = p.Member.User
	default:
		return errNoInteractionUser
	}
	return nil
}

// CommandName is the name of the command that ran ("" for other types).
func (i *Interaction) CommandName() string {
	if i.Type != InteractionCommand {
		return ""
	}
	return i.Data.Name
}

// Option returns the command option of that name, or nil. The getters of
// the result are safe on nil: i.Option("text").String().
func (i *Interaction) Option(name string) *InteractionOption {
	for k := range i.Data.Options {
		if i.Data.Options[k].Name == name {
			return &i.Data.Options[k]
		}
	}
	return nil
}

// CustomID is the custom ID of the component used, or of the modal
// submitted.
func (i *Interaction) CustomID() string { return i.Data.CustomID }

// Values are the values picked in a select.
func (i *Interaction) Values() []string { return i.Data.Values }

// Field returns the submitted value of a modal's text input ("" when absent).
func (i *Interaction) Field(customID string) string {
	for _, row := range i.Data.Components {
		for _, c := range row.Components {
			if c.CustomID == customID {
				return c.Value
			}
		}
	}
	return ""
}

// IsCommand reports a slash command.
func (i *Interaction) IsCommand() bool { return i.Type == InteractionCommand }

// IsComponent reports a click or a select.
func (i *Interaction) IsComponent() bool { return i.Type == InteractionComponent }

// IsButton reports a button click.
func (i *Interaction) IsButton() bool {
	return i.IsComponent() && i.Data.ComponentType == ComponentButton
}

// IsSelect reports a pick in any select menu.
func (i *Interaction) IsSelect() bool {
	if !i.IsComponent() {
		return false
	}
	switch i.Data.ComponentType {
	case ComponentStringSelect, ComponentUserSelect, ComponentRoleSelect, ComponentChannelSelect:
		return true
	}
	return false
}

// IsModalSubmit reports a submitted modal.
func (i *Interaction) IsModalSubmit() bool { return i.Type == InteractionModalSubmit }
