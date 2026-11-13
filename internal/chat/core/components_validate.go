package core

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/msch128/mnema-talk/internal/emojitext"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Limits of message components and modals (doc/bots.md, Message components
// and Modals), Discord's where it has one.
const (
	MaxComponentRows    = 5
	MaxRowButtons       = 5
	MaxCustomIDLen      = 100
	MaxButtonLabelLen   = 80
	MaxComponentURLLen  = 512
	MaxSelectOptions    = 25
	MaxOptionTextLen    = 100
	MaxPlaceholderLen   = 150
	MaxSelectValues     = 25
	MaxModalTitleLen    = 45
	MaxModalRows        = 5
	MaxInputLabelLen    = 45
	MaxInputLen         = 4000
	MaxInputPlaceholder = 100
)

// componentError is a refusal of a bot's components or modal. Only bots read
// it (the bot API answers it as Discord's invalid form body, 50035), like
// the refusals of embeds, so it is no sentence of the web app's catalogue.
type componentError string

func (e componentError) Error() string { return string(e) }

func formError(format string, args ...any) error {
	return componentError(fmt.Sprintf(format, args...))
}

// invalidForm turns a refusal into the 400 the routes answer.
func invalidForm(err error) error {
	return httpx.ErrInvalidInput(err.Error())
}

// ValidateComponents checks the components of a bot's message and returns
// them cleaned, with only the fields their type uses. No components is nil.
// A message has at most 5 action rows; a row holds 1 to 5 buttons or exactly
// one select menu. Text inputs belong in modals, and component types Mnema
// Talk does not offer (the mentionable select 7, the layout components) are
// refused, so a bot learns at once instead of posting a message with holes.
func ValidateComponents(rows []ActionRow) ([]ActionRow, error) {
	out, err := validateComponents(rows)
	if err != nil {
		return nil, invalidForm(err)
	}
	return out, nil
}

func validateComponents(rows []ActionRow) ([]ActionRow, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) > MaxComponentRows {
		return nil, formError("components: at most %d action rows", MaxComponentRows)
	}
	ids := map[string]bool{}
	out := make([]ActionRow, 0, len(rows))
	for i, row := range rows {
		at := fmt.Sprintf("components[%d]", i)
		if row.Type != ComponentActionRow {
			return nil, refuseType(at, row.Type, "only action rows (type 1) are allowed at the top level")
		}
		clean, err := validateRow(at, row.Components, ids)
		if err != nil {
			return nil, err
		}
		out = append(out, ActionRow{Type: ComponentActionRow, Components: clean})
	}
	return out, nil
}

// refuseType names the problem with a component of the wrong place or kind.
func refuseType(at string, typ int, where string) error {
	if !knownInRow(typ) && typ != ComponentActionRow && typ != ComponentTextInput {
		return formError("%s.type: component type %d is not supported", at, typ)
	}
	return formError("%s.type: %s", at, where)
}

func knownInRow(typ int) bool {
	switch typ {
	case ComponentButton, ComponentStringSelect, ComponentUserSelect, ComponentRoleSelect, ComponentChannelSelect:
		return true
	}
	return false
}

func isSelect(typ int) bool { return knownInRow(typ) && typ != ComponentButton }

func validateRow(at string, items []Component, ids map[string]bool) ([]Component, error) {
	if len(items) == 0 || len(items) > MaxRowButtons {
		return nil, formError("%s.components: an action row holds 1 to %d components", at, MaxRowButtons)
	}
	out := make([]Component, 0, len(items))
	for j, c := range items {
		cat := fmt.Sprintf("%s.components[%d]", at, j)
		if !knownInRow(c.Type) {
			return nil, refuseType(cat, c.Type, "an action row holds buttons and select menus")
		}
		if isSelect(c.Type) && len(items) != 1 {
			return nil, formError("%s: a select menu fills its action row alone", cat)
		}
		clean, err := validateItem(cat, c)
		if err != nil {
			return nil, err
		}
		if clean.CustomID != "" {
			if ids[clean.CustomID] {
				return nil, formError("%s.custom_id: %q is used twice in this message", cat, clean.CustomID)
			}
			ids[clean.CustomID] = true
		}
		out = append(out, clean)
	}
	return out, nil
}

func validateItem(at string, c Component) (Component, error) {
	if c.Type == ComponentButton {
		return validateButton(at, c)
	}
	if c.Type == ComponentStringSelect {
		return validateStringSelect(at, c)
	}
	return validateEntitySelect(at, c)
}

func validateButton(at string, c Component) (Component, error) {
	out := Component{Type: ComponentButton, Disabled: c.Disabled}
	if c.Style < ButtonPrimary || c.Style > ButtonLink {
		return out, formError("%s.style: a button's style is 1 to 5", at)
	}
	out.Style = c.Style
	var err error
	if out.URL, out.CustomID, err = buttonTarget(at, c); err != nil {
		return out, err
	}
	if out.Label, err = componentLabel(at+".label", c.Label, MaxButtonLabelLen, false); err != nil {
		return out, err
	}
	if out.Emoji, err = componentEmoji(at+".emoji", c.Emoji); err != nil {
		return out, err
	}
	if out.Label == "" && out.Emoji == nil {
		return out, formError("%s: a button needs a label or an emoji", at)
	}
	return out, nil
}

// buttonTarget is what a click on a button does: a link button opens its url,
// every other button reports its custom_id to the bot.
func buttonTarget(at string, c Component) (link, id string, err error) {
	if c.Style != ButtonLink {
		if c.URL != "" {
			return "", "", formError("%s.url: only a link button (style 5) has a url", at)
		}
		id, err = customID(at+".custom_id", c.CustomID)
		return "", id, err
	}
	if c.CustomID != "" {
		return "", "", formError("%s.custom_id: a link button has a url, not a custom_id", at)
	}
	link, err = componentURL(at+".url", c.URL)
	return link, "", err
}

func validateStringSelect(at string, c Component) (Component, error) {
	out, err := selectBase(at, c)
	if err != nil {
		return out, err
	}
	if len(c.Options) == 0 || len(c.Options) > MaxSelectOptions {
		return out, formError("%s.options: a string select has 1 to %d options", at, MaxSelectOptions)
	}
	seen := map[string]bool{}
	defaults := 0
	for k, o := range c.Options {
		opt, err := validateOption(fmt.Sprintf("%s.options[%d]", at, k), o)
		if err != nil {
			return out, err
		}
		if seen[opt.Value] {
			return out, formError("%s.options[%d].value: %q is used twice", at, k, opt.Value)
		}
		seen[opt.Value] = true
		if opt.Default {
			defaults++
		}
		out.Options = append(out.Options, opt)
	}
	lo, hi := SelectBounds(out)
	if hi > len(out.Options) || lo > len(out.Options) {
		return out, formError("%s: min_values and max_values cannot exceed the number of options", at)
	}
	if defaults > hi {
		return out, formError("%s.options: more options are default than max_values allows", at)
	}
	return out, nil
}

func validateOption(at string, o SelectOption) (SelectOption, error) {
	var out SelectOption
	var err error
	if out.Label, err = componentLabel(at+".label", o.Label, MaxOptionTextLen, true); err != nil {
		return out, err
	}
	if out.Value, err = botValue(at+".value", o.Value, MaxOptionTextLen); err != nil {
		return out, err
	}
	if out.Description, err = componentLabel(at+".description", o.Description, MaxOptionTextLen, false); err != nil {
		return out, err
	}
	if out.Emoji, err = componentEmoji(at+".emoji", o.Emoji); err != nil {
		return out, err
	}
	out.Default = o.Default
	return out, nil
}

func validateEntitySelect(at string, c Component) (Component, error) {
	if len(c.Options) > 0 {
		return Component{}, formError("%s.options: only a string select (type 3) has options", at)
	}
	return selectBase(at, c)
}

// selectBase checks what every select menu has: custom_id, placeholder and
// the bounds of how many values it takes.
func selectBase(at string, c Component) (Component, error) {
	out := Component{Type: c.Type, Disabled: c.Disabled, MinValues: c.MinValues, MaxValues: c.MaxValues}
	var err error
	if out.CustomID, err = customID(at+".custom_id", c.CustomID); err != nil {
		return out, err
	}
	if out.Placeholder, err = componentLabel(at+".placeholder", c.Placeholder, MaxPlaceholderLen, false); err != nil {
		return out, err
	}
	if c.MinValues != nil && (*c.MinValues < 0 || *c.MinValues > MaxSelectValues) {
		return out, formError("%s.min_values: 0 to %d", at, MaxSelectValues)
	}
	if c.MaxValues != nil && (*c.MaxValues < 1 || *c.MaxValues > MaxSelectValues) {
		return out, formError("%s.max_values: 1 to %d", at, MaxSelectValues)
	}
	if lo, hi := SelectBounds(out); lo > hi {
		return out, formError("%s: min_values cannot be above max_values", at)
	}
	return out, nil
}

// SelectBounds is how many values a select menu takes: min_values and
// max_values, each 1 when left out.
func SelectBounds(c Component) (lo, hi int) {
	lo, hi = 1, 1
	if c.MinValues != nil {
		lo = *c.MinValues
	}
	if c.MaxValues != nil {
		hi = *c.MaxValues
	}
	return lo, hi
}

// customID checks a custom_id: the bot's own value, returned to it exactly as
// it was given, so it is refused rather than changed.
func customID(field, s string) (string, error) {
	if s == "" {
		return "", formError("%s is required", field)
	}
	return botValue(field, s, MaxCustomIDLen)
}

// botValue checks a value the bot reads back (a custom_id, an option's
// value): 1 to max characters of valid UTF-8 without control characters,
// kept as it is.
func botValue(field, s string, max int) (string, error) {
	if s == "" {
		return "", formError("%s is required", field)
	}
	if !utf8.ValidString(s) || strings.ContainsFunc(s, isControl) {
		return "", formError("%s contains invalid characters", field)
	}
	if utf8.RuneCountInString(s) > max {
		return "", formError("%s must be at most %d characters", field, max)
	}
	return s, nil
}

func isControl(r rune) bool { return r < 0x20 || (r >= 0x7f && r <= 0x9f) }

// componentLabel cleans a text members read as part of the interface (a
// label, a placeholder, a title) like a name: one line, no invisible or
// bidirectional characters.
func componentLabel(field, s string, max int, required bool) (string, error) {
	// CleanName only cleans here; the refusals name the component's field.
	v, err := httpx.CleanName("title", s, len(s), false)
	switch {
	case err != nil:
		return "", formError("%s contains invalid characters", field)
	case required && v == "":
		return "", formError("%s is required", field)
	case utf8.RuneCountInString(v) > max:
		return "", formError("%s must be at most %d characters", field, max)
	}
	return v, nil
}

// componentEmoji checks a button's or an option's emoji: one Unicode emoji.
// Custom emoji are not offered on components.
func componentEmoji(field string, e *ComponentEmoji) (*ComponentEmoji, error) {
	if e == nil {
		return nil, nil
	}
	name := strings.TrimSpace(e.Name)
	if emojitext.IsToken(name) || !hasNonASCII(name) {
		return nil, formError("%s.name must be a Unicode emoji", field)
	}
	clean, err := ValidateEmoji(name)
	if err != nil {
		return nil, formError("%s.name must be a Unicode emoji", field)
	}
	return &ComponentEmoji{Name: clean}, nil
}

func hasNonASCII(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r >= utf8.RuneSelf })
}

// componentURL checks a link button's address: absolute http or https with a
// host, at most 512 bytes, nothing a link could hide.
func componentURL(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", formError("%s is required for a link button", field)
	}
	if len(s) > MaxComponentURLLen {
		return "", formError("%s must be at most %d bytes", field, MaxComponentURLLen)
	}
	u, err := url.Parse(s)
	bad := err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == ""
	if bad || strings.ContainsFunc(s, func(r rune) bool { return r <= 0x20 || r == 0x7f }) || !utf8.ValidString(s) {
		return "", formError("%s must be an http or https URL", field)
	}
	return s, nil
}

// FindComponent returns the component of rows with customID (link buttons
// have none).
func FindComponent(rows []ActionRow, customID string) (Component, bool) {
	for _, row := range rows {
		for _, c := range row.Components {
			if c.CustomID == customID && c.CustomID != "" {
				return c, true
			}
		}
	}
	return Component{}, false
}
