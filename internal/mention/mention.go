// Package mention reads "@name" mentions out of message text. Chat (who gets
// notified) and automod (mention spam) must agree on what a mention is, so
// both use this one definition.
package mention

import (
	"regexp"
	"strings"
)

// MinNameLen and MaxNameLen bound a mentioned name; longer or shorter tokens
// are plain text.
const (
	MinNameLen = 3
	MaxNameLen = 32
)

// token finds "@name" at the start of the text or after a character that
// cannot be part of a username (see auth.ValidateUsername).
var token = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])@([A-Za-z0-9_.-]{3,64})`)

// Names returns the lower-cased names mentioned in content, each once,
// including the keywords "all" and "here". A trailing '.' or '-' (end of a
// sentence) is not part of the name.
func Names(content string) []string {
	seen := map[string]bool{}
	var names []string
	for _, m := range token.FindAllStringSubmatch(content, -1) {
		name := strings.ToLower(strings.TrimRight(m[1], ".-"))
		if len(name) < MinNameLen || len(name) > MaxNameLen || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}
