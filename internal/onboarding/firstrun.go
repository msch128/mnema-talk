package onboarding

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Ids of the steps in the Owner's "Get started" checklist. The first five
// tick themselves from real state; "channels" and "status" tick once the Owner
// has opened that page of the console.
const (
	StepName       = "name"
	StepInvite     = "invite"
	StepChannels   = "channels"
	StepVoice      = "voice"
	StepStatus     = "status"
	StepRolesRules = "roles_rules"
)

// firstRunSteps lists the steps in the order shown; optional ones do not stop
// the checklist from counting as complete.
var firstRunSteps = []struct {
	id       string
	optional bool
}{
	{StepName, false}, {StepInvite, false}, {StepChannels, false}, {StepVoice, false}, {StepStatus, false}, {StepRolesRules, true},
}

// visitable are the steps only a visit can complete.
var visitable = []string{StepChannels, StepStatus}

// FirstRunStep is one line of the checklist.
type FirstRunStep struct {
	// ID names the step (name, invite, channels, voice, status, roles_rules).
	ID   string `json:"id" enum:"name,invite,channels,voice,status,roles_rules"`
	Done bool   `json:"done"`
	// Optional steps do not count towards Complete.
	Optional bool `json:"optional"`
}

// FirstRun is the Owner's first-run checklist.
type FirstRun struct {
	Steps []FirstRunStep `json:"steps"`
	// Hidden is true once the Owner closed the checklist (or when the community
	// was already in use before the checklist existed). The server menu
	// reopens it.
	Hidden bool `json:"hidden"`
	// Complete is true when every step that is not optional is done.
	Complete bool `json:"complete"`
}

// FirstRunInput is the body of PUT /api/admin/first-run.
type FirstRunInput struct {
	// Hidden closes (true) or reopens (false) the checklist; left out, it stays as it is.
	Hidden *bool `json:"hidden" binding:"optional" extensions:"x-nullable"`
	// Visited are steps the Owner has now opened: channels, status. They are added, never removed.
	Visited []string `json:"visited" binding:"optional" maxItems:"2"`
}

// storedFirstRun is the checklist's part of users.onboarding; Hidden stays nil
// until the Owner decides, so a community that was already in use can be told
// from a new one.
type storedFirstRun struct {
	Hidden  *bool    `json:"hidden,omitempty"`
	Visited []string `json:"visited"`
}

const firstRunKey = "first_run"

func readFirstRun(raw []byte) storedFirstRun {
	doc := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &doc)
	var s storedFirstRun
	_ = json.Unmarshal(doc[firstRunKey], &s)
	if s.Visited == nil {
		s.Visited = []string{}
	}
	return s
}

// withFirstRun writes s into the stored JSON and keeps every other key.
func withFirstRun(raw []byte, s storedFirstRun) ([]byte, error) {
	doc := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &doc)
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	doc[firstRunKey] = b
	return json.Marshal(doc)
}

// firstRunFacts are the real state the steps are read from.
type firstRunFacts struct {
	named, invited, lobby, rules bool
	// established: more than one account exists, so the community is not new.
	established bool
}

func (h *Handler) firstRunFacts(r *http.Request) (firstRunFacts, error) {
	var f firstRunFacts
	var accounts int
	err := h.DB.QueryRow(r.Context(), `
		SELECT s.community_name <> '', s.rules_text <> '',
		       EXISTS (SELECT 1 FROM invites),
		       EXISTS (SELECT 1 FROM channels WHERE is_lobby),
		       (SELECT COUNT(*) FROM users WHERE NOT is_bot)
		FROM instance_settings s`).Scan(&f.named, &f.rules, &f.invited, &f.lobby, &accounts)
	if err != nil {
		return f, fmt.Errorf("read first-run facts: %w", err)
	}
	f.established = accounts > 1
	f.invited = f.invited || f.established
	return f, nil
}

func buildFirstRun(f firstRunFacts, s storedFirstRun) FirstRun {
	done := map[string]bool{
		StepName: f.named, StepInvite: f.invited, StepVoice: f.lobby, StepRolesRules: f.rules,
		StepChannels: slices.Contains(s.Visited, StepChannels), StepStatus: slices.Contains(s.Visited, StepStatus),
	}
	out := FirstRun{Steps: make([]FirstRunStep, 0, len(firstRunSteps)), Complete: true}
	for _, st := range firstRunSteps {
		out.Steps = append(out.Steps, FirstRunStep{ID: st.id, Done: done[st.id], Optional: st.optional})
		if !st.optional && !done[st.id] {
			out.Complete = false
		}
	}
	// Not decided yet (only before the first look): a community with members
	// before the checklist existed does not get it handed to its Owner.
	out.Hidden = f.established
	if s.Hidden != nil {
		out.Hidden = *s.Hidden
	}
	return out
}

func (h *Handler) firstRun(r *http.Request) (FirstRun, error) {
	user := auth.UserFrom(r.Context())
	var raw []byte
	if err := h.DB.QueryRow(r.Context(), `SELECT onboarding FROM users WHERE id = $1`, user.ID).Scan(&raw); err != nil {
		return FirstRun{}, fmt.Errorf("read first-run record: %w", err)
	}
	f, err := h.firstRunFacts(r)
	if err != nil {
		return FirstRun{}, err
	}
	stored := readFirstRun(raw)
	if stored.Hidden == nil {
		// The first look decides once whether this community is new: later
		// members joining must not close a checklist the Owner never saw.
		if _, err := h.DB.Exec(r.Context(), `
			UPDATE users SET onboarding = jsonb_set(onboarding, '{first_run}',
			       COALESCE(onboarding->'first_run', '{}'::jsonb) || jsonb_build_object('hidden', $2::boolean))
			WHERE id = $1 AND onboarding->'first_run'->'hidden' IS NULL`, user.ID, f.established); err != nil {
			return FirstRun{}, fmt.Errorf("pin first-run default: %w", err)
		}
	}
	return buildFirstRun(f, stored), nil
}

// getFirstRun handles GET /api/admin/first-run.
//
// @Summary Read the Owner's first-run checklist
// @Description Owner only. The steps (name the community, invite people, review the channels, set up voice, check the status, optionally roles and rules), each with whether it is done, whether the Owner closed the checklist and whether every required step is done. Steps tick themselves from real state; channels and status once the Owner has opened that page. A community that already had members before the checklist existed starts closed.
// @ID getFirstRun
// @Tags Onboarding
// @Produce json
// @Security cookieAuth
// @Success 200 {object} FirstRun "Checklist."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may read it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/first-run [get]
func (h *Handler) getFirstRun(w http.ResponseWriter, r *http.Request) error {
	out, err := h.firstRun(r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// putFirstRun handles PUT /api/admin/first-run.
//
// @Summary Close, reopen or advance the Owner's first-run checklist
// @Description Owner only. `hidden` closes or reopens the checklist; `visited` adds the steps the Owner has opened (channels, status). Returns the checklist as it now stands.
// @ID putFirstRun
// @Tags Onboarding
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body FirstRunInput true "Changes."
// @Success 200 {object} FirstRun "Checklist."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner may change it, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/first-run [put]
func (h *Handler) putFirstRun(w http.ResponseWriter, r *http.Request) error {
	var in FirstRunInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	if err := validVisits(in.Visited); err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	f, err := h.firstRunFacts(r)
	if err != nil {
		return err
	}
	var out FirstRun
	err = pgx.BeginFunc(r.Context(), h.DB, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(r.Context(), `SELECT onboarding FROM users WHERE id = $1 FOR UPDATE`, user.ID).Scan(&raw); err != nil {
			return fmt.Errorf("lock first-run record: %w", err)
		}
		s := applyFirstRun(readFirstRun(raw), in, f)
		merged, err := withFirstRun(raw, s)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE users SET onboarding = $2 WHERE id = $1`, user.ID, merged); err != nil {
			return err
		}
		out = buildFirstRun(f, s)
		return nil
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func validVisits(ids []string) error {
	for _, id := range ids {
		if !slices.Contains(visitable, id) {
			return httpx.ErrInvalidInput("visited may only name channels or status")
		}
	}
	return nil
}

// applyFirstRun adds the visits to the stored record and sets the Owner's
// choice; with no choice yet it pins the default the first look would show.
func applyFirstRun(s storedFirstRun, in FirstRunInput, f firstRunFacts) storedFirstRun {
	for _, id := range in.Visited {
		if !slices.Contains(s.Visited, id) {
			s.Visited = append(s.Visited, id)
		}
	}
	switch {
	case in.Hidden != nil:
		s.Hidden = in.Hidden
	case s.Hidden == nil:
		s.Hidden = &f.established
	}
	return s
}
