package core

// Component types, with Discord's numbers (doc/bots.md, Message components).
const (
	ComponentActionRow     = 1
	ComponentButton        = 2
	ComponentStringSelect  = 3
	ComponentTextInput     = 4
	ComponentUserSelect    = 5
	ComponentRoleSelect    = 6
	ComponentChannelSelect = 8
)

// Button styles; ButtonLink carries a url instead of a custom_id.
const (
	ButtonPrimary   = 1
	ButtonSecondary = 2
	ButtonSuccess   = 3
	ButtonDanger    = 4
	ButtonLink      = 5
)

// Text input styles.
const (
	TextInputShort     = 1
	TextInputParagraph = 2
)

// ActionRow is one row of a bot message's components or of a modal (type 1),
// in Discord's shape: up to 5 buttons or one select menu, or in a modal one
// text input.
//
// These types are the wire shape that clients read in messages and events,
// so their schema carries no enums or length limits: a client must keep
// showing a message whose components a newer server or bot extended. What a
// bot may send is checked in components_validate.go (doc/bots.md).
type ActionRow struct {
	// Type is 1 (an action row).
	Type       int         `json:"type"`
	Components []Component `json:"components" binding:"optional"`
}

// Component is one element of an action row, in Discord's shape: a button, a
// select menu or a text input. Which fields apply depends on Type; the
// validation in components_validate.go refuses the others.
type Component struct {
	// Type is 2 button, 3 string select, 4 text input, 5 user select, 6 role
	// select or 8 channel select.
	Type int `json:"type"`
	// CustomID is the bot's own value, returned with every click (1 to 100 characters).
	CustomID string `json:"custom_id,omitempty" binding:"optional"`
	// Style of a button (1 to 5) or a text input (1 short, 2 paragraph).
	Style int    `json:"style,omitempty" binding:"optional"`
	Label string `json:"label,omitempty" binding:"optional"`
	// Emoji of a button: a Unicode emoji.
	Emoji *ComponentEmoji `json:"emoji,omitempty" binding:"optional"`
	// URL of a link button (http or https).
	URL      string `json:"url,omitempty" binding:"optional"`
	Disabled bool   `json:"disabled,omitempty" binding:"optional"`
	// Options of a string select (1 to 25).
	Options     []SelectOption `json:"options,omitempty" binding:"optional"`
	Placeholder string         `json:"placeholder,omitempty" binding:"optional"`
	// MinValues and MaxValues bound how many values a select takes (default 1).
	MinValues *int `json:"min_values,omitempty" binding:"optional"`
	MaxValues *int `json:"max_values,omitempty" binding:"optional"`
	// MinLength, MaxLength, Required and Value belong to a text input.
	MinLength *int   `json:"min_length,omitempty" binding:"optional"`
	MaxLength *int   `json:"max_length,omitempty" binding:"optional"`
	Required  *bool  `json:"required,omitempty" binding:"optional"`
	Value     string `json:"value,omitempty" binding:"optional"`
}

// ComponentEmoji is the emoji of a button.
type ComponentEmoji struct {
	Name string `json:"name" binding:"optional"`
}

// SelectOption is one choice of a string select.
type SelectOption struct {
	Label       string          `json:"label" binding:"optional"`
	Value       string          `json:"value" binding:"optional"`
	Description string          `json:"description,omitempty" binding:"optional"`
	Emoji       *ComponentEmoji `json:"emoji,omitempty" binding:"optional"`
	Default     bool            `json:"default,omitempty" binding:"optional"`
}

// Modal is a form a bot opens as the response to a command or a click
// (callback type 9): a title and 1 to 5 action rows of one text input each.
type Modal struct {
	CustomID   string      `json:"custom_id"`
	Title      string      `json:"title"`
	Components []ActionRow `json:"components"`
}
