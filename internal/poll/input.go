// Package poll implements polls: a question with 2 to 10 options that members
// vote on until it ends. A poll is a message (its content is the question)
// with rows in polls, poll_options and poll_votes; the read model that
// travels with the message is chat.Poll. Polls are created by the /poll
// command (internal/slash) or POST /api/channels/{id}/polls.
package poll

import (
	"strings"
	"time"
	"unicode"

	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// MaxQuestion bounds the question, in characters.
	MaxQuestion = 300
	// MaxLabel bounds one option, in characters.
	MaxLabel = 55
	// MinOptions and MaxOptions bound the number of options.
	MinOptions = 2
	MaxOptions = 10
	// DefaultDuration applies when none is given.
	DefaultDuration = "24h"
)

// Durations are the lengths a poll can run, by the name clients send.
var durations = map[string]time.Duration{
	"1h": time.Hour, "4h": 4 * time.Hour, "8h": 8 * time.Hour, "24h": 24 * time.Hour,
	"3d": 72 * time.Hour, "1w": 7 * 24 * time.Hour,
}

// DurationNames lists the accepted durations, shortest first.
func DurationNames() []string {
	return []string{"1h", "4h", "8h", "24h", "3d", "1w"}
}

// Input is a poll to create.
type Input struct {
	Question string
	Options  []string
	Multiple bool
	// Anonymous hides who voted for what.
	Anonymous bool
	// Duration is one of DurationNames; empty means DefaultDuration.
	Duration string
}

// clean is a validated poll: trimmed texts and the length as a time.Duration.
type clean struct {
	Input
	length time.Duration
}

func (in Input) clean() (clean, error) {
	q, err := httpx.CleanText("question", in.Question, MaxQuestion, true)
	if err != nil {
		return clean{}, err
	}
	labels, err := cleanLabels(in.Options)
	if err != nil {
		return clean{}, err
	}
	name := in.Duration
	if name == "" {
		name = DefaultDuration
	}
	length, ok := durations[name]
	if !ok {
		return clean{}, httpx.ErrInvalidInput("duration must be one of " + strings.Join(DurationNames(), ", "))
	}
	in.Question, in.Options, in.Duration = q, labels, name
	return clean{Input: in, length: length}, nil
}

func cleanLabels(options []string) ([]string, error) {
	if len(options) < MinOptions || len(options) > MaxOptions {
		return nil, httpx.ErrInvalidInput("a poll has 2 to 10 options")
	}
	out := make([]string, len(options))
	seen := map[string]bool{}
	for i, o := range options {
		label, err := httpx.CleanText("option", o, MaxLabel, true)
		if err != nil {
			return nil, err
		}
		if strings.IndexFunc(label, unicode.IsControl) >= 0 {
			return nil, httpx.ErrInvalidInput("an option is one line of text")
		}
		key := strings.ToLower(label)
		if seen[key] {
			return nil, httpx.ErrInvalidInput("options must differ")
		}
		seen[key] = true
		out[i] = label
	}
	return out, nil
}
