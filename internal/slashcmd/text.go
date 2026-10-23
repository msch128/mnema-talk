package slashcmd

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// textSpec describes a command that turns its text into a message.
type textSpec struct {
	name, description, optionDescription string
	// required says whether the text must be given (/shrug without one posts
	// the bare emoticon).
	required bool
	// render builds the message from the text, which is trimmed and may be empty
	// when the text is optional.
	render func(text string) string
}

// The shrug and the flip contain underscores and a backslash that the message
// markdown would take for formatting and escapes; they are written so that the
// rendered message shows exactly the familiar emoticon.
const (
	shrugFace     = `¯\\\_(ツ)_/¯`
	tableflipFace = `(╯°□°)╯︵ ┻━┻`
	unflipFace    = `┬─┬ノ( º \_ ºノ)`
)

var (
	// meSpec stores the text in italics, so clients that do not know the
	// command still show an action line.
	meSpec = textSpec{
		name: "me", description: "Describe what you are doing", optionDescription: "What you are doing", required: true,
		render: func(text string) string { return "_" + text + "_" },
	}
	shrugSpec     = textSpec{name: "shrug", description: "Add a shrug to your message", optionDescription: "Your message", render: withSuffix(shrugFace)}
	tableflipSpec = textSpec{name: "tableflip", description: "Flip a table", optionDescription: "Your message", render: withSuffix(tableflipFace)}
	unflipSpec    = textSpec{name: "unflip", description: "Put the table back", optionDescription: "Your message", render: withSuffix(unflipFace)}
	spoilerSpec   = textSpec{
		name: "spoiler", description: "Post a message hidden until someone clicks it", optionDescription: "The text to hide", required: true,
		render: func(text string) string { return "||" + text + "||" },
	}
)

func withSuffix(face string) func(string) string {
	return func(text string) string {
		if text == "" {
			return face
		}
		return text + " " + face
	}
}

// textCommand is /me, /shrug, /tableflip, /unflip and /spoiler.
type textCommand struct {
	svc  *Services
	spec textSpec
}

var _ slash.Builtin = textCommand{}

// Definition describes the command for the menu.
func (c textCommand) Definition() slash.Definition {
	return slash.Definition{
		Name: c.spec.name, Description: c.spec.description,
		Options: []slash.Option{{Name: "text", Description: c.spec.optionDescription, Type: slash.OptionString, Required: c.spec.required}},
	}
}

// Available: everyone who may send in the channel; the framework checked that.
func (textCommand) Available(*perm.Snapshot, perm.Subject, uuid.UUID) bool { return true }

// Run posts the message. The text goes through the same validation and mention
// handling as a message typed in the composer.
func (c textCommand) Run(_ context.Context, call slash.Call) (slash.Result, error) {
	text := strings.TrimSpace(str(call, "text"))
	if text == "" && c.spec.required {
		return slash.Result{}, httpx.ErrInvalidInput("missing option: text")
	}
	return c.svc.post(call, c.spec.render(text), nil)
}
