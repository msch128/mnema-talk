package discordimport

// Hint codes: the stable identifiers of every note and skip the plan can
// carry. Clients translate them; the English text stays next to them for
// clients that do not know a code.
const (
	NoteAnnouncementToText = "announcement_to_text"
	NoteStageToTalk        = "stage_to_talk"
	NoteForumToText        = "forum_to_text"
	NoteTopicShortened     = "topic_shortened"
	NoteSlowModeCapped     = "slow_mode_capped"
	NoteAgeRestriction     = "age_restriction_not_imported"

	SkipChannelTypeUnsupported = "channel_type_unsupported"
)

// NoteCodes and SkipCodes list every code the plan can emit.
var (
	NoteCodes = []string{NoteAnnouncementToText, NoteStageToTalk, NoteForumToText, NoteTopicShortened, NoteSlowModeCapped, NoteAgeRestriction}
	SkipCodes = []string{SkipChannelTypeUnsupported}
)

// Hint is a structured note or skip. Params carry raw values (names, counts),
// never translated text.
type Hint struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params"`
}

func hint(code string, params map[string]any) Hint {
	if params == nil {
		params = map[string]any{}
	}
	return Hint{Code: code, Params: params}
}

// note records a channel note: the structured hint and its English text.
func (pc *PlanChannel) note(code, text string, params map[string]any) {
	pc.Notes = append(pc.Notes, text)
	pc.NoteHints = append(pc.NoteHints, hint(code, params))
}
