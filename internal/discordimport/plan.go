package discordimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Discord channel types the import knows.
const (
	typeText         = 0
	typeVoice        = 2
	typeCategory     = 4
	typeAnnouncement = 5
	typeStage        = 13
	typeForum        = 15

	maxRoleName = 32
)

// Capabilities says which newer channel kinds this server has. Without them a
// stage becomes a normal Talk and a forum a text channel.
type Capabilities struct {
	Forum bool
	Stage bool
}

// Inputs are the facts about this server that shape a plan.
type Inputs struct {
	// Grantable is what the importing administrator may put on a role.
	Grantable perm.Set
	// ExistingRoles are the names of the roles that exist now.
	ExistingRoles []string
	// ServerName is the community name (empty when none is set).
	ServerName string
	Caps       Capabilities
}

// BuildPlan computes what importing t would create. It reads nothing and
// writes nothing.
func BuildPlan(t *Template, in Inputs) *Plan {
	p := &Plan{
		Code: t.Code, TemplateName: strings.TrimSpace(t.Name), SourceName: strings.TrimSpace(t.Guild.Name),
		ServerName: in.ServerName, Skipped: []string{},
	}
	b := &builder{plan: p, in: in, roleBySrc: map[string]*PlanRole{}, dropped: map[string]bool{}}
	b.roles(t.Guild.Roles)
	b.structure(t.Guild.Channels)
	b.finish()
	return p
}

type builder struct {
	plan      *Plan
	in        Inputs
	everyone  string
	roleBySrc map[string]*PlanRole
	dropped   map[string]bool
}

// roles plans the roles. Discord orders by position (higher is more
// powerful); template order breaks ties.
func (b *builder) roles(src []SrcRole) {
	order := make([]int, 0, len(src))
	for i, r := range src {
		if r.Name == "@everyone" {
			b.everyone = string(r.ID)
			m := mapBits(r.Permissions.Bits(), perm.All&^perm.Administrator)
			b.plan.Everyone = EveryoneInfo{PermissionKeys: keys(m.Set), Dropped: httpx.NonNil(m.Dropped)}
			continue
		}
		order = append(order, i)
	}
	sort.SliceStable(order, func(x, y int) bool { return src[order[x]].Position < src[order[y]].Position })
	taken := map[string]bool{}
	for _, n := range b.in.ExistingRoles {
		taken[strings.ToLower(strings.TrimSpace(n))] = true
	}
	b.plan.Roles = make([]PlanRole, 0, len(order))
	for _, i := range order {
		pr := b.role(src[i], taken)
		b.plan.Roles = append(b.plan.Roles, pr)
	}
	slices.Reverse(b.plan.Roles)
	for i := range b.plan.Roles {
		b.roleBySrc[b.plan.Roles[i].SourceID] = &b.plan.Roles[i]
	}
}

func (b *builder) role(r SrcRole, taken map[string]bool) PlanRole {
	m := mapBits(r.Permissions.Bits(), b.in.Grantable&perm.All)
	b.noteDropped(m.Dropped)
	b.plan.AdministratorDropped = b.plan.AdministratorDropped || m.AdministratorDropped
	return PlanRole{
		SourceID: string(r.ID), Name: uniqueRoleName(r.Name, taken), SourceName: strings.TrimSpace(r.Name),
		Color: clampColor(r.Color), Hoist: r.Hoist, Mentionable: r.Mentionable,
		Permissions: m.Set, PermissionKeys: keys(m.Set), Dropped: httpx.NonNil(m.Dropped),
		AdministratorDropped: m.AdministratorDropped,
	}
}

func (b *builder) noteDropped(names []string) {
	for _, n := range names {
		b.dropped[n] = true
	}
}

func clampColor(c int) int {
	if c < 0 || c > 0xFFFFFF {
		return 0
	}
	return c
}

// cleanName shortens s to max characters and strips what Mnema refuses; an
// empty result becomes fallback.
func cleanName(s string, max int, fallback string) string {
	s = truncate(strings.TrimSpace(s), max)
	if c, err := httpx.CleanName("name", s, max, true); err == nil {
		return c
	}
	return fallback
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max]))
}

// uniqueRoleName keeps a role's name unless it is reserved or an existing (or
// earlier imported) role already has it; then it gets a number suffix.
func uniqueRoleName(name string, taken map[string]bool) string {
	base := cleanName(name, maxRoleName, "Role")
	if reserved := strings.TrimPrefix(httpx.NameSkeleton(base), "@"); reserved == "everyone" || reserved == "here" {
		base = "Role"
	}
	candidate := base
	for n := 2; taken[strings.ToLower(candidate)]; n++ {
		suffix := " (" + strconv.Itoa(n) + ")"
		candidate = truncate(base, maxRoleName-utf8.RuneCountInString(suffix)) + suffix
	}
	taken[strings.ToLower(candidate)] = true
	return candidate
}

// structure plans categories and channels.
func (b *builder) structure(src []SrcChan) {
	cats, chans := splitChannels(src)
	catPlan := map[string]*PlanCategory{}
	b.plan.Categories = make([]PlanCategory, 0, len(cats))
	for _, c := range cats {
		b.plan.Categories = append(b.plan.Categories, PlanCategory{
			SourceID: string(c.ID), Name: cleanName(c.Name, chat.MaxChannelNameLen, "Category"),
			Overwrites: b.overwrites(c.Overwrite),
		})
	}
	for i := range b.plan.Categories {
		catPlan[b.plan.Categories[i].SourceID] = &b.plan.Categories[i]
	}
	b.plan.Channels = make([]PlanChannel, 0, len(chans))
	for _, parent := range append(sourceIDs(cats), "") {
		for _, c := range chans {
			if effectiveParent(c, catPlan) == parent {
				b.plan.Channels = append(b.plan.Channels, b.channel(c, catPlan[parent]))
			}
		}
	}
}

func sourceIDs(cats []SrcChan) []string {
	out := make([]string, len(cats))
	for i, c := range cats {
		out[i] = string(c.ID)
	}
	return out
}

func effectiveParent(c SrcChan, cats map[string]*PlanCategory) string {
	if _, ok := cats[string(c.ParentID)]; ok {
		return string(c.ParentID)
	}
	return ""
}

// splitChannels sorts categories and channels the way Discord shows them:
// by position, and inside a category text-like channels before Talks.
func splitChannels(src []SrcChan) (cats, chans []SrcChan) {
	for _, c := range src {
		if c.Type == typeCategory {
			cats = append(cats, c)
		} else {
			chans = append(chans, c)
		}
	}
	sort.SliceStable(cats, func(i, j int) bool { return cats[i].Position < cats[j].Position })
	sort.SliceStable(chans, func(i, j int) bool {
		vi, vj := isVoiceKind(chans[i].Type), isVoiceKind(chans[j].Type)
		if vi != vj {
			return vj
		}
		return chans[i].Position < chans[j].Position
	})
	return cats, chans
}

func isVoiceKind(t int) bool { return t == typeVoice || t == typeStage }

var sourceTypeNames = map[int]string{
	typeText: "text", typeVoice: "voice", typeAnnouncement: "announcement", typeStage: "stage", typeForum: "forum",
}

// channel plans one channel. Unsupported kinds are recorded as skipped and
// planned with an empty Type, which the caller filters out in finish.
func (b *builder) channel(c SrcChan, cat *PlanCategory) PlanChannel {
	pc := PlanChannel{SourceID: string(c.ID), Notes: []string{}}
	if cat != nil {
		pc.CategorySourceID, pc.Category = cat.SourceID, cat.Name
	}
	kind, ok := sourceTypeNames[c.Type]
	if !ok {
		pc.Type = ""
		b.plan.Skipped = append(b.plan.Skipped, fmt.Sprintf("%s (Discord channel type %d has no counterpart)", cleanName(c.Name, chat.MaxChannelNameLen, "channel"), c.Type))
		return pc
	}
	pc.SourceType = kind
	pc.Name = cleanName(c.Name, chat.MaxChannelNameLen, "channel")
	pc.Type = b.targetType(kind, &pc)
	pc.Topic = b.topic(c, &pc)
	if pc.Type == "voice" || pc.Type == "stage" {
		pc.UserLimit = min(max(c.UserLimit, 0), chat.MaxUserLimit)
	} else {
		pc.SlowModeSeconds = slowMode(c.RateLimit, &pc)
	}
	if c.NSFW {
		pc.Notes = append(pc.Notes, "the age restriction is not imported")
	}
	pc.Overwrites = b.overwrites(c.Overwrite)
	pc.Private = hidesFromEveryone(pc.Overwrites)
	return pc
}

// targetType decides what a Discord kind becomes here and notes the change.
func (b *builder) targetType(kind string, pc *PlanChannel) string {
	switch kind {
	case "announcement":
		pc.Notes = append(pc.Notes, "an announcement channel becomes a text channel")
		return "text"
	case "stage":
		if b.in.Caps.Stage {
			return "stage"
		}
		pc.Notes = append(pc.Notes, "a stage channel becomes a Talk")
		return "voice"
	case "forum":
		if b.in.Caps.Forum {
			return "forum"
		}
		pc.Notes = append(pc.Notes, "a forum channel becomes a text channel")
		return "text"
	case "voice":
		return "voice"
	}
	return "text"
}

func (b *builder) topic(c SrcChan, pc *PlanChannel) string {
	if c.Topic == nil {
		return ""
	}
	t := strings.TrimSpace(*c.Topic)
	if utf8.RuneCountInString(t) > chat.MaxTopicLen {
		pc.Notes = append(pc.Notes, "the topic was shortened")
		t = truncate(t, chat.MaxTopicLen)
	}
	if clean, err := httpx.CleanText("topic", t, chat.MaxTopicLen, false); err == nil {
		return clean
	}
	return ""
}

func slowMode(seconds int, pc *PlanChannel) int {
	if seconds <= 0 {
		return 0
	}
	if seconds > chat.MaxSlowModeSeconds {
		pc.Notes = append(pc.Notes, fmt.Sprintf("slow mode is capped at %d seconds", chat.MaxSlowModeSeconds))
		return chat.MaxSlowModeSeconds
	}
	return seconds
}

// overwrites maps role overwrites onto the planned roles. @everyone's
// overwrite applies to the new channel only, where it keeps private channels
// private. Member overwrites and overwrites of unknown roles are ignored.
func (b *builder) overwrites(src []SrcOverwrite) []PlanOverwrite {
	out := []PlanOverwrite{}
	for _, o := range src {
		if !o.forRole() {
			continue
		}
		po := PlanOverwrite{RoleSourceID: string(o.ID)}
		switch role := b.roleBySrc[string(o.ID)]; {
		case b.everyone != "" && string(o.ID) == b.everyone:
			po.Everyone, po.RoleName = true, "@everyone"
		case role != nil:
			po.RoleName = role.Name
		default:
			continue
		}
		allow, deny := mapBits(o.Allow.Bits(), perm.ChannelScoped), mapBits(o.Deny.Bits(), perm.ChannelScoped)
		b.noteDropped(allow.Dropped)
		b.noteDropped(deny.Dropped)
		po.Allow, po.Deny = allow.Set&^deny.Set, deny.Set
		if po.Allow == 0 && po.Deny == 0 {
			continue
		}
		po.AllowKeys, po.DenyKeys = keys(po.Allow), keys(po.Deny)
		out = append(out, po)
	}
	return out
}

func hidesFromEveryone(ows []PlanOverwrite) bool {
	for _, o := range ows {
		if o.Everyone && o.Deny.Has(perm.ViewChannel) {
			return true
		}
	}
	return false
}

// finish drops skipped channels, collects the dropped bits and seals the plan
// with its counts and hash.
func (b *builder) finish() {
	p := b.plan
	p.Channels = slices.DeleteFunc(p.Channels, func(c PlanChannel) bool { return c.Type == "" })
	p.DroppedBits = make([]string, 0, len(b.dropped))
	for n := range b.dropped {
		p.DroppedBits = append(p.DroppedBits, n)
	}
	sort.Strings(p.DroppedBits)
	p.Counts = Counts{Categories: len(p.Categories), Channels: len(p.Channels), Roles: len(p.Roles)}
	p.Hash = ""
	raw, _ := json.Marshal(p) // plain data: cannot fail
	sum := sha256.Sum256(raw)
	p.Hash = hex.EncodeToString(sum[:])
}
