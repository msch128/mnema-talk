package mnema

import (
	"errors"
	"fmt"
	"net/url"
	"unicode/utf8"
)

// Limits of components and modals (doc/bots.md, Message components, Modals).
const (
	MaxActionRows        = 5
	MaxComponents        = 25
	MaxButtonsPerRow     = 5
	MaxCustomIDLength    = 100
	MaxButtonLabelLength = 80
	MaxSelectOptions     = 25
	MaxOptionTextLength  = 100
	MaxPlaceholderLength = 150
	MaxSelectValues      = 25
	MaxModalTitleLength  = 45
	MaxModalRows         = 5
	MaxInputLabelLength  = 45
	MaxInputTextLength   = 4000
	MaxInputPlaceholder  = 100
)

// ErrInvalidComponent is wrapped by every validation error of components and
// modals.
var ErrInvalidComponent = errors.New("mnema: invalid component")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidComponent, fmt.Sprintf(format, args...))
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// ValidateComponents checks the components of a message: at most 5 action
// rows and 25 components, each row valid, and custom IDs unique.
func ValidateComponents(rows []Component) error {
	if len(rows) > MaxActionRows {
		return invalid("%d action rows, at most %d", len(rows), MaxActionRows)
	}
	total := 0
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Type != ComponentActionRow {
			return invalid("top-level components must be action rows, got type %d", row.Type)
		}
		if err := row.Validate(); err != nil {
			return err
		}
		total += len(row.Components)
		if err := uniqueIDs(row.Components, seen); err != nil {
			return err
		}
	}
	if total > MaxComponents {
		return invalid("%d components, at most %d", total, MaxComponents)
	}
	return nil
}

func uniqueIDs(list []Component, seen map[string]bool) error {
	for _, c := range list {
		if c.CustomID == "" {
			continue
		}
		if seen[c.CustomID] {
			return invalid("custom_id %q is used twice", c.CustomID)
		}
		seen[c.CustomID] = true
	}
	return nil
}

// Validate checks one component against the documented limits. An action row
// is checked as a row of a message (buttons or one select).
func (c Component) Validate() error {
	switch c.Type {
	case ComponentActionRow:
		return validateMessageRow(c)
	case ComponentButton:
		return validateButton(c)
	case ComponentStringSelect:
		return validateStringSelect(c)
	case ComponentUserSelect, ComponentRoleSelect, ComponentChannelSelect:
		return validateSelectBase(c)
	case ComponentTextInput:
		return validateTextInput(c)
	default:
		return invalid("unknown component type %d", c.Type)
	}
}

func validateMessageRow(row Component) error {
	n := len(row.Components)
	if n == 0 {
		return invalid("an action row needs at least one component")
	}
	buttons := 0
	for _, ch := range row.Components {
		switch ch.Type {
		case ComponentButton:
			buttons++
		case ComponentActionRow, ComponentTextInput:
			return invalid("component type %d cannot be in a message row", ch.Type)
		}
		if err := ch.Validate(); err != nil {
			return err
		}
	}
	if buttons != n && n != 1 {
		return invalid("a select menu must be alone in its row")
	}
	if buttons > MaxButtonsPerRow {
		return invalid("%d buttons in a row, at most %d", buttons, MaxButtonsPerRow)
	}
	return nil
}

func validateCustomID(id string) error {
	if id == "" || runes(id) > MaxCustomIDLength {
		return invalid("custom_id must be 1 to %d characters", MaxCustomIDLength)
	}
	return nil
}

func validateButton(c Component) error {
	if runes(c.Label) > MaxButtonLabelLength {
		return invalid("button label longer than %d characters", MaxButtonLabelLength)
	}
	if c.Label == "" && (c.Emoji == nil || c.Emoji.Name == "") {
		return invalid("a button needs a label or an emoji")
	}
	switch ButtonStyle(c.Style) {
	case ButtonPrimary, ButtonSecondary, ButtonSuccess, ButtonDanger:
		if c.URL != "" {
			return invalid("only link buttons carry a url")
		}
		return validateCustomID(c.CustomID)
	case ButtonLink:
		if c.CustomID != "" {
			return invalid("a link button has no custom_id")
		}
		return validateLinkURL(c.URL)
	default:
		return invalid("button style %d, want 1 to 5", c.Style)
	}
}

func validateLinkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return invalid("a link button needs an http or https url")
	}
	return nil
}

func validateSelectBase(c Component) error {
	if err := validateCustomID(c.CustomID); err != nil {
		return err
	}
	if runes(c.Placeholder) > MaxPlaceholderLength {
		return invalid("placeholder longer than %d characters", MaxPlaceholderLength)
	}
	minV, maxV := 1, 1
	if c.MinValues != nil {
		minV = *c.MinValues
	}
	if c.MaxValues != nil {
		maxV = *c.MaxValues
	}
	if minV < 0 || minV > MaxSelectValues || maxV < 1 || maxV > MaxSelectValues {
		return invalid("min_values must be 0 to %d and max_values 1 to %d", MaxSelectValues, MaxSelectValues)
	}
	if minV > maxV {
		return invalid("min_values %d is above max_values %d", minV, maxV)
	}
	return nil
}

func validateStringSelect(c Component) error {
	if err := validateSelectBase(c); err != nil {
		return err
	}
	if len(c.Options) == 0 || len(c.Options) > MaxSelectOptions {
		return invalid("a string select needs 1 to %d options", MaxSelectOptions)
	}
	values := map[string]bool{}
	for _, o := range c.Options {
		if err := validateOption(o); err != nil {
			return err
		}
		if values[o.Value] {
			return invalid("option value %q is used twice", o.Value)
		}
		values[o.Value] = true
	}
	return nil
}

func validateOption(o SelectOption) error {
	if o.Label == "" || runes(o.Label) > MaxOptionTextLength || o.Value == "" || runes(o.Value) > MaxOptionTextLength {
		return invalid("option label and value must be 1 to %d characters", MaxOptionTextLength)
	}
	if runes(o.Description) > MaxOptionTextLength {
		return invalid("option description longer than %d characters", MaxOptionTextLength)
	}
	return nil
}

func validateTextInput(c Component) error {
	if err := validateCustomID(c.CustomID); err != nil {
		return err
	}
	if c.Label == "" || runes(c.Label) > MaxInputLabelLength {
		return invalid("text input label must be 1 to %d characters", MaxInputLabelLength)
	}
	if TextInputStyle(c.Style) != TextInputShort && TextInputStyle(c.Style) != TextInputParagraph {
		return invalid("text input style %d, want 1 or 2", c.Style)
	}
	minL, maxL := 0, MaxInputTextLength
	if c.MinLength != nil {
		minL = *c.MinLength
	}
	if c.MaxLength != nil {
		maxL = *c.MaxLength
	}
	if minL < 0 || minL > MaxInputTextLength || maxL < 1 || maxL > MaxInputTextLength || minL > maxL {
		return invalid("min_length must be 0 to %d, max_length 1 to %d, min not above max", MaxInputTextLength, MaxInputTextLength)
	}
	if runes(c.Value) > MaxInputTextLength || runes(c.Placeholder) > MaxInputPlaceholder {
		return invalid("value up to %d and placeholder up to %d characters", MaxInputTextLength, MaxInputPlaceholder)
	}
	return nil
}

// Validate checks the modal: title 1 to 45 characters, a custom ID, and 1 to
// 5 rows of exactly one text input each, with unique custom IDs.
func (m Modal) Validate() error {
	if err := validateCustomID(m.CustomID); err != nil {
		return err
	}
	if m.Title == "" || runes(m.Title) > MaxModalTitleLength {
		return invalid("modal title must be 1 to %d characters", MaxModalTitleLength)
	}
	if len(m.Components) == 0 || len(m.Components) > MaxModalRows {
		return invalid("a modal needs 1 to %d rows", MaxModalRows)
	}
	seen := map[string]bool{}
	for _, row := range m.Components {
		if row.Type != ComponentActionRow || len(row.Components) != 1 || row.Components[0].Type != ComponentTextInput {
			return invalid("each modal row holds exactly one text input")
		}
		if err := row.Components[0].Validate(); err != nil {
			return err
		}
		if err := uniqueIDs(row.Components, seen); err != nil {
			return err
		}
	}
	return nil
}
