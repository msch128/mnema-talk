package poll

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// answerSeparator splits the answers option of /poll.
const answerSeparator = "|"

// Command is /poll, the built-in command that posts a poll:
// /poll question: ... answers: Yes | No | Maybe [duration: 24h] [multiple] [anonymous].
type Command struct {
	Service *Service
}

var _ slash.Builtin = Command{}

// Definition describes /poll for the menu.
func (Command) Definition() slash.Definition {
	return slash.Definition{
		Name:        "poll",
		Description: "Ask the channel a question with up to 10 answers",
		Options: []slash.Option{
			{Name: "question", Description: "What do you want to ask?", Type: slash.OptionString, Required: true},
			{Name: "answers", Description: "2 to 10 answers, separated by |", Type: slash.OptionString, Required: true},
			{Name: "duration", Description: "How long voting stays open", Type: slash.OptionString, Choices: DurationNames()},
			{Name: "multiple", Description: "Allow several answers per member", Type: slash.OptionBoolean},
			{Name: "anonymous", Description: "Hide who voted for what", Type: slash.OptionBoolean},
		},
	}
}

// Available: everyone who may send in the channel may ask.
func (Command) Available(*perm.Snapshot, perm.Subject, uuid.UUID) bool { return true }

// Run posts the poll.
func (c Command) Run(ctx context.Context, call slash.Call) (slash.Result, error) {
	ctx, err := chat.SendContext(c.Service.Perms, call.Request, call.Channel, perm.SendMessages)
	if err != nil {
		return slash.Result{}, err
	}
	in := Input{Question: str(call.Options["question"]), Options: splitAnswers(str(call.Options["answers"])), Duration: str(call.Options["duration"])}
	in.Multiple, _ = call.Options["multiple"].(bool)
	in.Anonymous, _ = call.Options["anonymous"].(bool)
	msg, err := c.Service.Create(ctx, call.User, call.Channel, in)
	if err != nil {
		return slash.Result{}, err
	}
	return slash.Result{MessageID: &msg.ID}, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// splitAnswers cuts "a | b | c" into its answers; empty pieces are dropped.
func splitAnswers(s string) []string {
	var out []string
	for _, part := range strings.Split(s, answerSeparator) {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}
