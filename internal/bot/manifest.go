package bot

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	// ManifestVersion is the only manifest version this server understands.
	ManifestVersion = 1
	// MaxDescriptionLen bounds a bot's description (bots.description).
	MaxDescriptionLen = 200
	// MaxHomepageLen bounds a bot's homepage (bots.homepage).
	MaxHomepageLen = 512
)

// Manifest is the JSON document in which a bot describes itself so that an
// admin can add it (doc/bots.md, "Adding a bot from a manifest"). Unknown
// fields are ignored, at every level, so a newer manifest still installs on an
// older server with what that server understands.
type Manifest struct {
	ManifestVersion int    `json:"manifest_version" enums:"1"`
	Name            string `json:"name" minLength:"1" maxLength:"24"`
	Description     string `json:"description,omitempty" binding:"optional" maxLength:"200"`
	// Homepage is an https URL, shown as a link.
	Homepage string `json:"homepage,omitempty" binding:"optional" maxLength:"512"`
	// Avatar is a data: URL the web app uploads through the avatar route. The
	// server ignores it and never fetches anything.
	Avatar string `json:"avatar,omitempty" binding:"optional"`
	// Intents are names: GUILDS, GUILD_MEMBERS, GUILD_MESSAGES, GUILD_MESSAGE_REACTIONS.
	Intents []string `json:"intents,omitempty" binding:"optional"`
	// Permissions are permission keys, channel permissions only: view_channel,
	// send_messages, attach_files, add_reactions, manage_messages, mention_everyone.
	Permissions []string `json:"permissions,omitempty" binding:"optional"`
	// Commands are registered as by PUT /api/bot/v1/commands; at most 50.
	Commands []CommandInput `json:"commands,omitempty" binding:"optional"`
}

// UnmarshalJSON decodes the manifest leniently: httpx.DecodeJSON refuses
// unknown fields, but a manifest must tolerate them (see Manifest). A type
// mismatch of a known field is still an error.
func (m *Manifest) UnmarshalJSON(b []byte) error {
	type plain Manifest
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*m = Manifest(p)
	return nil
}

// ManifestIntent is one gateway intent of a checked manifest.
type ManifestIntent struct {
	Name string `json:"name" enums:"GUILDS,GUILD_MEMBERS,GUILD_MESSAGES,GUILD_MESSAGE_REACTIONS"`
	Bit  int64  `json:"bit" minimum:"1"`
}

// ManifestPermission is one permission of a checked manifest.
type ManifestPermission struct {
	Key string `json:"key"`
	// Name is the permission in words, as in error messages.
	Name string   `json:"name"`
	Bit  perm.Set `json:"bit" swaggertype:"integer" minimum:"1"`
}

// CheckedManifest is a manifest as the server understood it: cleaned, with
// the names of its intents and permissions, in a fixed order, without
// duplicates and without the fields the server ignores (the avatar included).
type CheckedManifest struct {
	ManifestVersion int                  `json:"manifest_version" enums:"1"`
	Name            string               `json:"name" minLength:"1" maxLength:"24"`
	Description     string               `json:"description" maxLength:"200"`
	Homepage        string               `json:"homepage" maxLength:"512"`
	Intents         []ManifestIntent     `json:"intents"`
	Permissions     []ManifestPermission `json:"permissions"`
	Commands        []CommandInput       `json:"commands"`
}

// manifestIntents are the intents a manifest may ask for, in bit order.
var manifestIntents = []ManifestIntent{
	{"GUILDS", IntentGuilds},
	{"GUILD_MEMBERS", IntentGuildMembers},
	{"GUILD_MESSAGES", IntentGuildMessages},
	{"GUILD_MESSAGE_REACTIONS", IntentGuildMessageReactions},
}

// ManifestPermissions are the only permissions a manifest may ask for: what a
// bot uses in channels. Administrator, the admin area and voice stay a
// deliberate grant by hand.
const ManifestPermissions = perm.ViewChannel | perm.SendMessages | perm.AttachFiles |
	perm.AddReactions | perm.ManageMessages | perm.MentionEveryone

// Check validates the manifest and returns it normalised.
func (m Manifest) Check() (CheckedManifest, error) {
	if m.ManifestVersion != ManifestVersion {
		return CheckedManifest{}, httpx.ErrInvalidInput(fmt.Sprintf("manifest_version must be %d", ManifestVersion))
	}
	out := CheckedManifest{ManifestVersion: ManifestVersion}
	var err error
	if out.Name, err = cleanName(m.Name); err != nil {
		return CheckedManifest{}, err
	}
	if out.Description, err = httpx.CleanText("description", m.Description, MaxDescriptionLen, false); err != nil {
		return CheckedManifest{}, err
	}
	if out.Homepage, err = cleanHomepage(m.Homepage); err != nil {
		return CheckedManifest{}, err
	}
	if out.Intents, err = intentsByName(m.Intents); err != nil {
		return CheckedManifest{}, err
	}
	if out.Permissions, err = permissionsByKey(m.Permissions); err != nil {
		return CheckedManifest{}, err
	}
	if out.Commands, err = cleanManifestCommands(m.Commands); err != nil {
		return CheckedManifest{}, err
	}
	return out, nil
}

// cleanHomepage accepts an absolute https URL with a host and without
// credentials; empty stays empty.
func cleanHomepage(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if len(s) > MaxHomepageLen {
		return "", httpx.ErrInvalidInput(fmt.Sprintf("homepage must be at most %d characters", MaxHomepageLen))
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(s, " \t\r\n") {
		return "", httpx.ErrInvalidInput("homepage must be an https URL")
	}
	return s, nil
}

// intentsByName maps intent names to the known intents, in bit order.
func intentsByName(names []string) ([]ManifestIntent, error) {
	out := []ManifestIntent{}
	for _, in := range manifestIntents {
		if slices.Contains(names, in.Name) {
			out = append(out, in)
		}
	}
	for _, n := range names {
		if !slices.ContainsFunc(manifestIntents, func(in ManifestIntent) bool { return in.Name == n }) {
			return nil, httpx.ErrInvalidInput("unknown intent: " + clipForError(n))
		}
	}
	return out, nil
}

// permissionsByKey maps permission keys to the catalog, in its order, and
// refuses any key a manifest may not ask for.
func permissionsByKey(keys []string) ([]ManifestPermission, error) {
	for _, k := range keys {
		i, ok := catalogByKey(k)
		switch {
		case !ok:
			return nil, httpx.ErrInvalidInput("unknown permission: " + clipForError(k))
		case !ManifestPermissions.Has(i.Bit):
			return nil, httpx.ErrInvalidInput("a manifest cannot ask for this permission: " + k)
		}
	}
	out := []ManifestPermission{}
	for _, i := range perm.Catalog {
		if slices.Contains(keys, i.Key) {
			out = append(out, ManifestPermission{Key: i.Key, Name: i.Name, Bit: i.Bit})
		}
	}
	return out, nil
}

func catalogByKey(key string) (perm.Info, bool) {
	for _, i := range perm.Catalog {
		if i.Key == key {
			return i, true
		}
	}
	return perm.Info{}, false
}

// clipForError keeps an error message short whatever a manifest contains.
func clipForError(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

// cleanManifestCommands validates the commands as PUT /api/bot/v1/commands
// does: each one, at most MaxCommands, no name twice.
func cleanManifestCommands(in []CommandInput) ([]CommandInput, error) {
	if _, err := collectUniqueCommandNames(in); err != nil {
		return nil, err
	}
	out, err := cleanAll(in)
	if err != nil {
		return nil, err
	}
	if _, err := collectUniqueCommandNames(out); err != nil {
		return nil, err
	}
	return out, nil
}

// bits is the union of the checked manifest's permissions.
func (c CheckedManifest) bits() perm.Set {
	var s perm.Set
	for _, p := range c.Permissions {
		s |= p.Bit
	}
	return s
}

// intentBits is the union of the checked manifest's intents.
func (c CheckedManifest) intentBits() int64 {
	var s int64
	for _, i := range c.Intents {
		s |= i.Bit
	}
	return s
}
