package core

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// ValidateModal checks a modal a bot opens (callback type 9) and returns it
// cleaned: a custom_id, a title of up to 45 characters and 1 to 5 action rows
// of exactly one text input each.
func ValidateModal(m Modal) (Modal, error) {
	out, err := validateModal(m)
	if err != nil {
		return Modal{}, invalidForm(err)
	}
	return out, nil
}

func validateModal(m Modal) (Modal, error) {
	var out Modal
	var err error
	if out.CustomID, err = customID("data.custom_id", m.CustomID); err != nil {
		return out, err
	}
	if out.Title, err = componentLabel("data.title", m.Title, MaxModalTitleLen, true); err != nil {
		return out, err
	}
	if len(m.Components) == 0 || len(m.Components) > MaxModalRows {
		return out, formError("data.components: a modal has 1 to %d action rows", MaxModalRows)
	}
	ids := map[string]bool{}
	for i, row := range m.Components {
		at := fmt.Sprintf("data.components[%d]", i)
		if row.Type != ComponentActionRow {
			return out, refuseType(at, row.Type, "only action rows (type 1) are allowed at the top level")
		}
		if len(row.Components) != 1 || row.Components[0].Type != ComponentTextInput {
			return out, formError("%s.components: a modal's action row holds exactly one text input (type 4)", at)
		}
		input, err := validateTextInput(at+".components[0]", row.Components[0])
		if err != nil {
			return out, err
		}
		if ids[input.CustomID] {
			return out, formError("%s.components[0].custom_id: %q is used twice in this modal", at, input.CustomID)
		}
		ids[input.CustomID] = true
		out.Components = append(out.Components, ActionRow{Type: ComponentActionRow, Components: []Component{input}})
	}
	return out, nil
}

func validateTextInput(at string, c Component) (Component, error) {
	out := Component{Type: ComponentTextInput, MinLength: c.MinLength, MaxLength: c.MaxLength, Required: c.Required}
	var err error
	if out.CustomID, err = customID(at+".custom_id", c.CustomID); err != nil {
		return out, err
	}
	if out.Label, err = componentLabel(at+".label", c.Label, MaxInputLabelLen, true); err != nil {
		return out, err
	}
	if c.Style != TextInputShort && c.Style != TextInputParagraph {
		return out, formError("%s.style: 1 (short) or 2 (paragraph)", at)
	}
	out.Style = c.Style
	if out.Placeholder, err = componentLabel(at+".placeholder", c.Placeholder, MaxInputPlaceholder, false); err != nil {
		return out, err
	}
	if err := checkInputBounds(at, c); err != nil {
		return out, err
	}
	if c.Value != "" {
		if out.Value, err = inputText(at+".value", c.Value, c.Style); err != nil {
			return out, err
		}
		if _, hi := InputBounds(out); utf8.RuneCountInString(out.Value) > hi {
			return out, formError("%s.value is longer than max_length", at)
		}
	}
	return out, nil
}

func checkInputBounds(at string, c Component) error {
	if c.MinLength != nil && (*c.MinLength < 0 || *c.MinLength > MaxInputLen) {
		return formError("%s.min_length: 0 to %d", at, MaxInputLen)
	}
	if c.MaxLength != nil && (*c.MaxLength < 1 || *c.MaxLength > MaxInputLen) {
		return formError("%s.max_length: 1 to %d", at, MaxInputLen)
	}
	if lo, hi := InputBounds(c); lo > hi {
		return formError("%s: min_length cannot be above max_length", at)
	}
	return nil
}

// InputBounds is how long a text input's answer may be: min_length (0 when
// left out) to max_length (4000).
func InputBounds(c Component) (lo, hi int) {
	lo, hi = 0, MaxInputLen
	if c.MinLength != nil {
		lo = *c.MinLength
	}
	if c.MaxLength != nil {
		hi = *c.MaxLength
	}
	return lo, hi
}

// InputRequired reports whether a text input must be filled in (the default).
func InputRequired(c Component) bool { return c.Required == nil || *c.Required }

// inputText checks the text of a text input: valid UTF-8 without control
// characters; a paragraph keeps its line breaks and tabs, a short input has
// none.
func inputText(field, s string, style int) (string, error) {
	if !utf8.ValidString(s) {
		return "", formError("%s contains invalid characters", field)
	}
	allowed := func(r rune) bool { return style == TextInputParagraph && (r == '\n' || r == '\t' || r == '\r') }
	if strings.ContainsFunc(s, func(r rune) bool { return isControl(r) && !allowed(r) }) {
		return "", formError("%s contains invalid characters", field)
	}
	if utf8.RuneCountInString(s) > MaxInputLen {
		return "", formError("%s must be at most %d characters", field, MaxInputLen)
	}
	return s, nil
}

// CheckInputAnswer checks what a member typed into a text input of a modal:
// the text rules, the length bounds and whether it is required. The answer is
// trimmed of surrounding white space first, as the member sees it.
func CheckInputAnswer(input Component, answer string) (string, error) {
	v := strings.TrimSpace(answer)
	if _, err := inputText("answer", v, input.Style); err != nil {
		return "", httpx.ErrInvalidInput("an answer contains characters that are not allowed")
	}
	n := utf8.RuneCountInString(v)
	if n == 0 {
		if InputRequired(input) {
			return "", httpx.ErrInvalidInput("fill in every required field")
		}
		return "", nil
	}
	if lo, hi := InputBounds(input); n < lo || n > hi {
		return "", httpx.ErrInvalidInput(fmt.Sprintf("an answer must be %d to %d characters", lo, hi))
	}
	return v, nil
}
