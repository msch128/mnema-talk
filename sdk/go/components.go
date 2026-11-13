package mnema

// ComponentType is the type of a message component (Discord's numbers).
type ComponentType int

// The component types the server offers.
const (
	ComponentActionRow     ComponentType = 1
	ComponentButton        ComponentType = 2
	ComponentStringSelect  ComponentType = 3
	ComponentTextInput     ComponentType = 4
	ComponentUserSelect    ComponentType = 5
	ComponentRoleSelect    ComponentType = 6
	ComponentChannelSelect ComponentType = 8
)

// ButtonStyle is the style of a button.
type ButtonStyle int

// The button styles; ButtonLink carries a URL instead of a custom ID.
const (
	ButtonPrimary   ButtonStyle = 1
	ButtonSecondary ButtonStyle = 2
	ButtonSuccess   ButtonStyle = 3
	ButtonDanger    ButtonStyle = 4
	ButtonLink      ButtonStyle = 5
)

// TextInputStyle is the style of a modal's text input.
type TextInputStyle int

// The text input styles.
const (
	TextInputShort     TextInputStyle = 1
	TextInputParagraph TextInputStyle = 2
)

// Component is one element of a message's components or of a modal, in
// Discord's JSON: an action row, a button, a select menu or a text input.
// Which fields apply depends on Type. The builders (ActionRow, Button,
// StringSelect, ...) fill it; Validate checks the documented limits.
type Component struct {
	Type        ComponentType  `json:"type"`
	Components  []Component    `json:"components,omitempty"`
	CustomID    string         `json:"custom_id,omitempty"`
	Style       int            `json:"style,omitempty"`
	Label       string         `json:"label,omitempty"`
	Emoji       *Emoji         `json:"emoji,omitempty"`
	URL         string         `json:"url,omitempty"`
	Disabled    bool           `json:"disabled,omitempty"`
	Options     []SelectOption `json:"options,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"`
	MinValues   *int           `json:"min_values,omitempty"`
	MaxValues   *int           `json:"max_values,omitempty"`
	MinLength   *int           `json:"min_length,omitempty"`
	MaxLength   *int           `json:"max_length,omitempty"`
	Required    *bool          `json:"required,omitempty"`
	Value       string         `json:"value,omitempty"`
}

// Emoji is the Unicode emoji of a button or select option.
type Emoji struct {
	Name string `json:"name"`
}

// SelectOption is one choice of a string select.
type SelectOption struct {
	Label       string `json:"label"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
	Emoji       *Emoji `json:"emoji,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

// ComponentBuilder is anything that becomes a Component: the builder types
// below and Component itself.
type ComponentBuilder interface {
	Component() Component
}

// Component returns c itself, so a Component can be passed where a builder is
// expected.
func (c Component) Component() Component { return c }

// ActionRow returns an action row with the given children: up to 5 buttons,
// exactly one select menu, or (in a modal) one text input.
func ActionRow(children ...ComponentBuilder) Component {
	row := Component{Type: ComponentActionRow, Components: make([]Component, 0, len(children))}
	for _, ch := range children {
		row.Components = append(row.Components, ch.Component())
	}
	return row
}

// Button is a button. Interactive styles carry a CustomID; ButtonLink carries
// a URL instead. Emoji is an optional Unicode emoji.
type Button struct {
	Style    ButtonStyle
	Label    string
	CustomID string
	URL      string
	Emoji    string
	Disabled bool
}

// Component returns the button's JSON form. A zero Style means primary, or
// link when only a URL is set.
func (b Button) Component() Component {
	style := b.Style
	if style == 0 {
		style = ButtonPrimary
		if b.URL != "" && b.CustomID == "" {
			style = ButtonLink
		}
	}
	c := Component{Type: ComponentButton, Style: int(style), Label: b.Label, CustomID: b.CustomID, URL: b.URL, Disabled: b.Disabled}
	if b.Emoji != "" {
		c.Emoji = &Emoji{Name: b.Emoji}
	}
	return c
}

// Validate checks the button against the server's limits.
func (b Button) Validate() error { return b.Component().Validate() }

// PrimaryButton returns a primary button.
func PrimaryButton(customID, label string) Button {
	return Button{Style: ButtonPrimary, CustomID: customID, Label: label}
}

// SecondaryButton returns a secondary button.
func SecondaryButton(customID, label string) Button {
	return Button{Style: ButtonSecondary, CustomID: customID, Label: label}
}

// SuccessButton returns a success (green) button.
func SuccessButton(customID, label string) Button {
	return Button{Style: ButtonSuccess, CustomID: customID, Label: label}
}

// DangerButton returns a danger (red) button.
func DangerButton(customID, label string) Button {
	return Button{Style: ButtonDanger, CustomID: customID, Label: label}
}

// LinkButton returns a button that opens url (http or https).
func LinkButton(url, label string) Button {
	return Button{Style: ButtonLink, URL: url, Label: label}
}

// StringSelect is a select menu of fixed options. MinValues and MaxValues
// default to 1 when nil.
type StringSelect struct {
	CustomID    string
	Placeholder string
	Options     []SelectOption
	MinValues   *int
	MaxValues   *int
	Disabled    bool
}

// Component returns the select's JSON form.
func (s StringSelect) Component() Component {
	return Component{Type: ComponentStringSelect, CustomID: s.CustomID, Placeholder: s.Placeholder, Options: s.Options,
		MinValues: s.MinValues, MaxValues: s.MaxValues, Disabled: s.Disabled}
}

// Validate checks the select against the server's limits.
func (s StringSelect) Validate() error { return s.Component().Validate() }

// selectMenu holds the fields shared by user, role and channel selects.
type selectMenu struct {
	CustomID    string
	Placeholder string
	MinValues   *int
	MaxValues   *int
	Disabled    bool
}

func (s selectMenu) component(t ComponentType) Component {
	return Component{Type: t, CustomID: s.CustomID, Placeholder: s.Placeholder, MinValues: s.MinValues, MaxValues: s.MaxValues, Disabled: s.Disabled}
}

// UserSelect is a select menu of members.
type UserSelect selectMenu

// Component returns the select's JSON form.
func (s UserSelect) Component() Component { return selectMenu(s).component(ComponentUserSelect) }

// Validate checks the select against the server's limits.
func (s UserSelect) Validate() error { return s.Component().Validate() }

// RoleSelect is a select menu of roles (not @everyone, not the managed bot
// roles).
type RoleSelect selectMenu

// Component returns the select's JSON form.
func (s RoleSelect) Component() Component { return selectMenu(s).component(ComponentRoleSelect) }

// Validate checks the select against the server's limits.
func (s RoleSelect) Validate() error { return s.Component().Validate() }

// ChannelSelect is a select menu of text, forum and announcement channels.
type ChannelSelect selectMenu

// Component returns the select's JSON form.
func (s ChannelSelect) Component() Component { return selectMenu(s).component(ComponentChannelSelect) }

// Validate checks the select against the server's limits.
func (s ChannelSelect) Validate() error { return s.Component().Validate() }

// TextInput is a text field of a modal. Required defaults to true when nil.
// A zero Style means short.
type TextInput struct {
	CustomID    string
	Label       string
	Style       TextInputStyle
	MinLength   *int
	MaxLength   *int
	Required    *bool
	Value       string
	Placeholder string
}

// Component returns the text input's JSON form.
func (t TextInput) Component() Component {
	style := t.Style
	if style == 0 {
		style = TextInputShort
	}
	return Component{Type: ComponentTextInput, CustomID: t.CustomID, Label: t.Label, Style: int(style), MinLength: t.MinLength,
		MaxLength: t.MaxLength, Required: t.Required, Value: t.Value, Placeholder: t.Placeholder}
}

// Validate checks the text input against the server's limits.
func (t TextInput) Validate() error { return t.Component().Validate() }

// Modal is a form opened with Interaction.ShowModal: a title and 1 to 5 rows
// of one text input each. Rows may be given as TextInputs (each becomes a
// row) through NewModal, or as action rows directly.
type Modal struct {
	CustomID   string      `json:"custom_id"`
	Title      string      `json:"title"`
	Components []Component `json:"components"`
}

// NewModal returns a modal with one row per text input.
func NewModal(customID, title string, inputs ...TextInput) Modal {
	m := Modal{CustomID: customID, Title: title}
	for _, in := range inputs {
		m.Components = append(m.Components, ActionRow(in))
	}
	return m
}
