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

// Component is one element of a bot message's components or of a modal, in
// Discord's shape: an action row (Components), a button, a select menu or a
// text input. Which fields apply depends on Type; the validation in
// components_validate.go refuses the others.
type Component struct {
	Type int `json:"type" enums:"1,2,3,4,5,6,8"`
	// Components are the children of an action row.
	Components []Component `json:"components,omitempty" binding:"optional"`
	// CustomID is the bot's own value, returned with every click (1 to 100 characters).
	CustomID string `json:"custom_id,omitempty" binding:"optional" maxLength:"100"`
	// Style of a button (1 to 5) or a text input (1 short, 2 paragraph).
	Style int    `json:"style,omitempty" binding:"optional" enums:"1,2,3,4,5"`
	Label string `json:"label,omitempty" binding:"optional" maxLength:"80"`
	// Emoji of a button: a Unicode emoji.
	Emoji *ComponentEmoji `json:"emoji,omitempty" binding:"optional"`
	// URL of a link button (http or https).
	URL      string `json:"url,omitempty" binding:"optional" maxLength:"512"`
	Disabled bool   `json:"disabled,omitempty" binding:"optional"`
	// Options of a string select (1 to 25).
	Options     []SelectOption `json:"options,omitempty" binding:"optional"`
	Placeholder string         `json:"placeholder,omitempty" binding:"optional" maxLength:"150"`
	// MinValues and MaxValues bound how many values a select takes (default 1).
	MinValues *int `json:"min_values,omitempty" binding:"optional" minimum:"0" maximum:"25"`
	MaxValues *int `json:"max_values,omitempty" binding:"optional" minimum:"1" maximum:"25"`
	// MinLength, MaxLength, Required and Value belong to a text input.
	MinLength *int   `json:"min_length,omitempty" binding:"optional" minimum:"0" maximum:"4000"`
	MaxLength *int   `json:"max_length,omitempty" binding:"optional" minimum:"1" maximum:"4000"`
	Required  *bool  `json:"required,omitempty" binding:"optional"`
	Value     string `json:"value,omitempty" binding:"optional" maxLength:"4000"`
}

// ComponentEmoji is the emoji of a button.
type ComponentEmoji struct {
	Name string `json:"name"`
}

// SelectOption is one choice of a string select.
type SelectOption struct {
	Label       string          `json:"label" maxLength:"100"`
	Value       string          `json:"value" maxLength:"100"`
	Description string          `json:"description,omitempty" binding:"optional" maxLength:"100"`
	Emoji       *ComponentEmoji `json:"emoji,omitempty" binding:"optional"`
	Default     bool            `json:"default,omitempty" binding:"optional"`
}

// Modal is a form a bot opens as the response to a command or a click
// (callback type 9): a title and 1 to 5 action rows of one text input each.
type Modal struct {
	CustomID   string      `json:"custom_id" maxLength:"100"`
	Title      string      `json:"title" maxLength:"45"`
	Components []Component `json:"components"`
}
