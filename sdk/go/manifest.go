package mnema

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Manifest describes a bot so that an admin can add it from an add link or
// by pasting it (doc/bots.md, Adding a bot from a manifest).
type Manifest struct {
	// ManifestVersion must be 1; AddBotLink sets it when it is 0.
	ManifestVersion int `json:"manifest_version"`
	// Name is 1 to 24 characters.
	Name string `json:"name"`
	// Description is up to 200 characters.
	Description string `json:"description,omitempty"`
	// Homepage is an https URL of up to 512 characters.
	Homepage string `json:"homepage,omitempty"`
	// Avatar is a data: URL of a PNG, JPEG or WebP image of at most 256 KB.
	Avatar string `json:"avatar,omitempty"`
	// Intents are names such as "GUILDS"; Intents.Names() gives them.
	Intents []string `json:"intents,omitempty"`
	// Permissions are channel permissions: view_channel, send_messages,
	// attach_files, add_reactions, manage_messages, mention_everyone.
	Permissions []string `json:"permissions,omitempty"`
	// Commands are registered as with SetCommands (at most 50).
	Commands []CommandInput `json:"commands,omitempty"`
}

var (
	manifestIntents     = map[string]bool{"GUILDS": true, "GUILD_MEMBERS": true, "GUILD_MESSAGES": true, "GUILD_MESSAGE_REACTIONS": true}
	manifestPermissions = map[string]bool{"view_channel": true, "send_messages": true, "attach_files": true, "add_reactions": true, "manage_messages": true, "mention_everyone": true}
	avatarPattern       = regexp.MustCompile(`^data:image/(png|jpeg|webp);base64,`)
	commandNamePattern  = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
)

// ErrInvalidManifest is wrapped by every manifest validation error.
var ErrInvalidManifest = errors.New("mnema: invalid manifest")

func badManifest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidManifest, fmt.Sprintf(format, args...))
}

// Validate checks the manifest against the server's rules.
func (m Manifest) Validate() error {
	if m.ManifestVersion != 1 {
		return badManifest("manifest_version must be 1")
	}
	if n := runes(m.Name); n < 1 || n > 24 {
		return badManifest("name must be 1 to 24 characters")
	}
	if runes(m.Description) > 200 {
		return badManifest("description must be at most 200 characters")
	}
	if m.Homepage != "" && (!strings.HasPrefix(m.Homepage, "https://") || len(m.Homepage) > 512) {
		return badManifest("homepage must be an https URL of at most 512 characters")
	}
	if m.Avatar != "" && !avatarPattern.MatchString(m.Avatar) {
		return badManifest("avatar must be a data: URL of a PNG, JPEG or WebP image")
	}
	if err := checkNames("intent", m.Intents, manifestIntents); err != nil {
		return err
	}
	if err := checkNames("permission", m.Permissions, manifestPermissions); err != nil {
		return err
	}
	return validateCommands(m.Commands)
}

func checkNames(kind string, list []string, known map[string]bool) error {
	for _, x := range list {
		if !known[x] {
			return badManifest("unknown %s %q", kind, x)
		}
	}
	return nil
}

func validateCommands(cmds []CommandInput) error {
	if len(cmds) > 50 {
		return badManifest("at most 50 commands")
	}
	for _, c := range cmds {
		if !commandNamePattern.MatchString(c.Name) {
			return badManifest("command name %q must be 1 to 32 lowercase letters, digits, - or _", c.Name)
		}
		if len(c.Options) > 10 {
			return badManifest("command %q has more than 10 options", c.Name)
		}
	}
	return nil
}

// AddBotLink returns the link that opens the server's confirm screen for the
// manifest: <serverURL>/admin/bots/add#manifest=<base64url JSON>. The
// manifest is in the fragment, which the browser never sends to the server.
func AddBotLink(serverURL string, m Manifest) (string, error) {
	if m.ManifestVersion == 0 {
		m.ManifestVersion = 1
	}
	if err := m.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("mnema: encode manifest: %w", err)
	}
	return strings.TrimRight(serverURL, "/") + "/admin/bots/add#manifest=" + base64.RawURLEncoding.EncodeToString(b), nil
}
