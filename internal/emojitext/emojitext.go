// Package emojitext knows how a custom emoji is written in message text and
// reactions: the token <:name:id>. The id is stable, the name is only the
// fallback shown when the emoji is renamed away or deleted.
package emojitext

import (
	"regexp"

	"github.com/google/uuid"
)

// NamePattern is what an emoji name looks like.
const NamePattern = `[a-z0-9_]{2,32}`

// MaxTokenLen is the longest token: "<:", 32 name characters, ":", a UUID, ">".
const MaxTokenLen = 2 + 32 + 1 + 36 + 1

var (
	nameRE  = regexp.MustCompile(`^` + NamePattern + `$`)
	tokenRE = regexp.MustCompile(`<:(` + NamePattern + `):([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>`)
	wholeRE = regexp.MustCompile(`^` + tokenRE.String() + `$`)
)

// ValidName reports whether name is a legal emoji name.
func ValidName(name string) bool { return nameRE.MatchString(name) }

// Token writes the token of an emoji.
func Token(name string, id uuid.UUID) string { return "<:" + name + ":" + id.String() + ">" }

// IsToken reports whether s is exactly one token.
func IsToken(s string) bool { return wholeRE.MatchString(s) }

// Plain replaces every token in text by :name:, for places that show text
// without images (push notifications, previews).
func Plain(text string) string { return tokenRE.ReplaceAllString(text, ":$1:") }
