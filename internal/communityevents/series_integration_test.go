//go:build integration

package communityevents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

// occurrences lists the series' events by index: start, status, title.
type occurrence struct {
	ID     uuid.UUID
	Index  int
	Start  time.Time
	Status string
	Title  string
}

func (f *rosterFixture) occurrences(t *testing.T, series uuid.UUID) []occurrence {
	t.Helper()
	rows, err := f.p.Query(context.Background(), `SELECT id, series_index, start_time, status, title FROM events WHERE series_id = $1 ORDER BY series_index`, series)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []occurrence
	for rows.Next() {
		var o occurrence
		if err := rows.Scan(&o.ID, &o.Index, &o.Start, &o.Status, &o.Title); err != nil {
			t.Fatal(err)
		}
		out = append(out, o)
	}
	return out
}

// startSeries creates a weekly series through the handler, as the API does, and returns its first occurrence.
func (f *rosterFixture) startSeries(t *testing.T, body string) *CommunityEvent {
	t.Helper()
	req := fmt.Sprintf(body, time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
	rec, err := f.call(t, f.owner, f.h.createEvent, http.MethodPost, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ev CommunityEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &ev); err != nil {
		t.Fatal(err)
	}
	f.event = &ev
	return &ev
}

const weeklyBody = `{"title":"Raid night","description":"d","start_time":%q,"end_time":null,"location":"Hall","recurrence":{"frequency":"weekly","timezone":"Europe/Berlin"}}`

func TestARecurringEventCreatesItsOccurrencesAheadAndOnlyOnce(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	if first.Series == nil || first.Series.Index != 0 || first.Series.Frequency != FreqWeekly || first.Series.Timezone != "Europe/Berlin" {
		t.Fatalf("series info: %+v", first.Series)
	}
	list := f.occurrences(t, first.Series.ID)
	if len(list) != 9 { // today + 48h, then weekly within 60 days
		t.Fatalf("occurrences: %d", len(list))
	}
	for i, o := range list {
		if o.Index != i || o.Title != "Raid night" || (i > 0 && !o.Start.Equal(list[0].Start.AddDate(0, 0, 7*i)) && o.Start.Sub(list[i-1].Start) < 167*time.Hour) {
			t.Fatalf("occurrence %d: %+v", i, o)
		}
	}
	// Nothing new until time passes; then the next ones appear, never twice.
	ctx := context.Background()
	if ids, err := f.s.GenerateSeries(ctx, first.Series.ID, time.Now()); err != nil || len(ids) != 0 {
		t.Fatalf("second run: %v %v", ids, err)
	}
	ids, err := f.s.GenerateSeries(ctx, first.Series.ID, time.Now().Add(30*24*time.Hour))
	if err != nil || len(ids) < 4 {
		t.Fatalf("a month later: %v %v", ids, err)
	}
	// A server that was down for weeks does not create the missed occurrences late.
	late, err := f.s.GenerateSeries(ctx, first.Series.ID, time.Now().Add(200*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range late {
		ev, _ := f.s.Get(ctx, id, nil)
		if ev.StartTime.Before(time.Now().Add(200 * 24 * time.Hour)) {
			t.Fatalf("occurrence %s started before the time it was created at", id)
		}
	}
}

func TestOccurrencesCopyTheRolesButNotTheSignups(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	f.event = first
	if _, err := f.call(t, f.owner, f.h.setRoles, http.MethodPut, `{"roles":[{"name":"Tank","capacity":1}]}`, nil); err != nil {
		t.Fatal(err)
	}
	roles, _ := f.s.Get(context.Background(), first.ID, nil)
	a := f.user(t, "a", "user")
	if _, _, err := f.s.SignUp(context.Background(), first.ID, a.ID, roles.Roles[0].ID); err != nil {
		t.Fatal(err)
	}

	// With scope=following the later occurrences (without sign-ups) get the roles, and so does the template.
	rec, err := f.callQuery(t, f.owner, f.h.setRoles, http.MethodPut, `{"roles":[{"id":"`+roles.Roles[0].ID.String()+`","name":"Tank","capacity":1},{"name":"Heal","capacity":2}]}`, "scope=following")
	if err != nil {
		t.Fatalf("%v %s", err, rec.Body.String())
	}
	list := f.occurrences(t, first.Series.ID)
	later, _ := f.s.Get(context.Background(), list[1].ID, &a.ID)
	if len(later.Roles) != 2 || later.Roles[1].Name != "Heal" || later.Roles[0].Confirmed != 0 || later.MySignup != nil {
		t.Fatalf("a later occurrence has the roles and nobody signed up: %+v %+v", later.Roles, later.MySignup)
	}
	// An occurrence that has sign-ups keeps its roles; the template still reaches the ones created later.
	sign := f.occurrenceRole(t, list[2].ID)
	if _, _, err := f.s.SignUp(context.Background(), list[2].ID, a.ID, sign); err != nil {
		t.Fatal(err)
	}
	f.event = &CommunityEvent{ID: list[1].ID}
	if _, err := f.callQuery(t, f.owner, f.h.setRoles, http.MethodPut, `{"roles":[{"name":"Solo"}]}`, "scope=following"); err != nil {
		t.Fatal(err)
	}
	kept, _ := f.s.Get(context.Background(), list[2].ID, nil)
	if len(kept.Roles) != 2 {
		t.Fatalf("an occurrence with sign-ups keeps its roles: %+v", kept.Roles)
	}
	ids, err := f.s.GenerateSeries(context.Background(), first.Series.ID, time.Now().Add(40*24*time.Hour))
	if err != nil || len(ids) == 0 {
		t.Fatalf("%v %v", ids, err)
	}
	fresh, _ := f.s.Get(context.Background(), ids[0], nil)
	if len(fresh.Roles) != 1 || fresh.Roles[0].Name != "Solo" || fresh.Roles[0].Confirmed != 0 {
		t.Fatalf("a new occurrence starts from the template: %+v", fresh.Roles)
	}
}

func (f *rosterFixture) occurrenceRole(t *testing.T, eventID uuid.UUID) uuid.UUID {
	t.Helper()
	ev, err := f.s.Get(context.Background(), eventID, nil)
	if err != nil || len(ev.Roles) == 0 {
		t.Fatalf("roles of %s: %v %v", eventID, ev, err)
	}
	return ev.Roles[0].ID
}

func TestEditingThisEventOrThisAndFollowing(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	list := f.occurrences(t, first.Series.ID)
	third := list[2]

	// "This event": only that occurrence changes.
	f.event = &CommunityEvent{ID: third.ID}
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{"title":"Special"}`, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.occurrences(t, first.Series.ID); got[2].Title != "Special" || got[1].Title != "Raid night" || got[3].Title != "Raid night" {
		t.Fatalf("only the third changes: %+v", titlesOf(got))
	}

	// "This and following": the third and all later ones (also the one edited alone), not the earlier ones.
	oldFourth := list[3].Start
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{"title":"New name","description":"new"}`, "scope=following"); err != nil {
		t.Fatal(err)
	}
	got := f.occurrences(t, first.Series.ID)
	for i, o := range got {
		want := "New name"
		if i < 2 {
			want = "Raid night"
		}
		if o.Title != want {
			t.Fatalf("occurrence %d is %q, want %q", i, o.Title, want)
		}
	}
	if !got[3].Start.Equal(oldFourth) {
		t.Fatal("without a new start_time the times stay")
	}
	// The template follows: occurrences created later carry the new name.
	ids, err := f.s.GenerateSeries(context.Background(), first.Series.ID, time.Now().Add(80*24*time.Hour))
	if err != nil || len(ids) == 0 {
		t.Fatal(err)
	}
	if ev, _ := f.s.Get(context.Background(), ids[0], nil); ev.Title != "New name" || ev.Description != "new" {
		t.Fatalf("new occurrence: %+v", ev)
	}
}

func titlesOf(list []occurrence) []string {
	out := make([]string, len(list))
	for i, o := range list {
		out[i] = o.Title
	}
	return out
}

func TestANewStartTimeMovesTheFollowingOccurrencesAndKeepsTheEarlierOnes(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	list := f.occurrences(t, first.Series.ID)
	f.event = &CommunityEvent{ID: list[2].ID}
	newStart := list[2].Start.Add(26 * time.Hour) // the next day, two hours later
	body := fmt.Sprintf(`{"start_time":%q}`, newStart.Format(time.RFC3339))
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, body, "scope=following"); err != nil {
		t.Fatal(err)
	}
	got := f.occurrences(t, first.Series.ID)
	for i := 0; i < 2; i++ {
		if !got[i].Start.Equal(list[i].Start) {
			t.Fatalf("occurrence %d is before the edit and keeps its time", i)
		}
	}
	for i := 2; i < len(got); i++ {
		if want := newStart.AddDate(0, 0, 7*(i-2)); got[i].Start.Sub(want).Abs() > time.Hour { // DST may add an hour
			t.Fatalf("occurrence %d starts %v, want about %v", i, got[i].Start, want)
		}
	}
	// New occurrences continue from the new anchor.
	ids, err := f.s.GenerateSeries(context.Background(), first.Series.ID, time.Now().Add(80*24*time.Hour))
	if err != nil || len(ids) == 0 {
		t.Fatal(err)
	}
	last := got[len(got)-1].Start
	if ev, _ := f.s.Get(context.Background(), ids[0], nil); ev.StartTime.Sub(last).Abs() < 6*24*time.Hour {
		t.Fatalf("the next occurrence keeps the weekly distance: %v after %v", ev.StartTime, last)
	}
}

func TestCancellingThisAndFollowingEndsTheSeries(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	list := f.occurrences(t, first.Series.ID)
	f.event = &CommunityEvent{ID: list[3].ID}

	if _, err := f.callQuery(t, f.owner, f.h.cancelEvent, http.MethodPost, ``, "scope=following"); err != nil {
		t.Fatal(err)
	}
	got := f.occurrences(t, first.Series.ID)
	for i, o := range got {
		if (i >= 3) != (o.Status == StatusCancelled) {
			t.Fatalf("occurrence %d is %s", i, o.Status)
		}
	}
	ids, err := f.s.GenerateSeries(context.Background(), first.Series.ID, time.Now().Add(300*24*time.Hour))
	if err != nil || len(ids) != 0 {
		t.Fatalf("an ended series creates nothing: %v %v", ids, err)
	}
	if ev, _ := f.s.Get(context.Background(), list[0].ID, nil); ev.Series == nil || !ev.Series.Ended {
		t.Fatalf("the series says it ended: %+v", ev.Series)
	}
	// Cancelling again finds nothing open.
	if _, err := f.callQuery(t, f.owner, f.h.cancelEvent, http.MethodPost, ``, "scope=following"); err == nil {
		t.Fatal("nothing is left to cancel")
	}
}

func TestSeriesLimitsAndScopeChecks(t *testing.T) {
	f := newRosterFixture(t)
	for name, body := range map[string]string{
		"bad frequency": `{"title":"x","description":"","start_time":%q,"location":"Hall","recurrence":{"frequency":"daily"}}`,
		"bad zone":      `{"title":"x","description":"","start_time":%q,"location":"Hall","recurrence":{"frequency":"weekly","timezone":"Mars/Base"}}`,
		"until before":  `{"title":"x","description":"","start_time":%q,"location":"Hall","recurrence":{"frequency":"weekly","until":"2020-01-01T00:00:00Z"}}`,
	} {
		req := fmt.Sprintf(body, time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339))
		if _, err := f.call(t, f.owner, f.h.createEvent, http.MethodPost, req, nil); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}

	// until ends the series.
	until := time.Now().Add(20 * 24 * time.Hour).UTC().Format(time.RFC3339)
	first := f.startSeries(t, `{"title":"x","description":"","start_time":%q,"location":"Hall","recurrence":{"frequency":"weekly","until":"`+until+`"}}`)
	if got := f.occurrences(t, first.Series.ID); len(got) != 3 {
		t.Fatalf("occurrences until the end date: %d", len(got))
	}

	// A single event takes no following scope; an unknown scope is refused.
	plain, err := f.s.Create(context.Background(), CreateEventRequest{Title: "single", StartTime: time.Now().Add(time.Hour), Location: ptr("Hall")}, f.creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.event = plain
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{"title":"y"}`, "scope=following"); err == nil {
		t.Fatal("following needs a series")
	}
	if _, err := f.callQuery(t, f.owner, f.h.cancelEvent, http.MethodPost, ``, "scope=forever"); err == nil {
		t.Fatal("an unknown scope is refused")
	}
}

func TestTheSchedulerCreatesOccurrencesAsTimePasses(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	before := len(f.occurrences(t, first.Series.ID))
	s := &Scheduler{Handler: f.h}
	s.Tick(context.Background(), time.Now().Add(30*24*time.Hour))
	after := len(f.occurrences(t, first.Series.ID))
	if after <= before {
		t.Fatalf("a month later there are more occurrences: %d then %d", before, after)
	}
	s.Tick(context.Background(), time.Now().Add(30*24*time.Hour+time.Minute))
	if again := len(f.occurrences(t, first.Series.ID)); again != after {
		t.Fatalf("the series are looked at every ten minutes, not every tick: %d then %d", after, again)
	}
	s.Tick(context.Background(), time.Now().Add(90*24*time.Hour))
	if later := len(f.occurrences(t, first.Series.ID)); later <= after {
		t.Fatalf("ten minutes on, the next ones follow: %d then %d", after, later)
	}
}

func TestDurationsPlacesAndStatusEditsOfAFollowingScope(t *testing.T) {
	f := newRosterFixture(t)
	body := `{"title":"Movie","description":"","start_time":%q,"end_time":"` + time.Now().Add(50*time.Hour).UTC().Format(time.RFC3339) + `","location":"Hall","recurrence":{"frequency":"biweekly"}}`
	first := f.startSeries(t, body)
	list := f.occurrences(t, first.Series.ID)
	if len(list) != 5 {
		t.Fatalf("biweekly within 60 days: %d", len(list))
	}
	ev, _ := f.s.Get(context.Background(), list[1].ID, nil)
	if ev.EndTime == nil || ev.EndTime.Sub(ev.StartTime) != 2*time.Hour {
		t.Fatalf("the duration is copied: %v %v", ev.StartTime, ev.EndTime)
	}

	// A new end time changes the duration of the following occurrences; a new place moves them.
	f.event = &CommunityEvent{ID: list[1].ID}
	newEnd := ev.StartTime.Add(3 * time.Hour).Format(time.RFC3339)
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, fmt.Sprintf(`{"end_time":%q,"location":"Cinema"}`, newEnd), "scope=following"); err != nil {
		t.Fatal(err)
	}
	later, _ := f.s.Get(context.Background(), list[3].ID, nil)
	if later.EndTime == nil || later.EndTime.Sub(later.StartTime) != 3*time.Hour || later.Location == nil || *later.Location != "Cinema" {
		t.Fatalf("following occurrence: %+v", later)
	}
	if kept, _ := f.s.Get(context.Background(), list[0].ID, nil); *kept.Location != "Hall" {
		t.Fatal("the earlier one keeps its place")
	}
	// Removing the end time removes it from the following ones.
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{"end_time":null}`, "scope=following"); err != nil {
		t.Fatal(err)
	}
	if later, _ = f.s.Get(context.Background(), list[3].ID, nil); later.EndTime != nil {
		t.Fatal("no end time any more")
	}

	// A move to a Talk, an edit that changes nothing and a cancellation by status.
	talk := f.newTalk(t)
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, fmt.Sprintf(`{"channel_id":%q}`, talk), "scope=following"); err != nil {
		t.Fatal(err)
	}
	if moved, _ := f.s.Get(context.Background(), list[2].ID, nil); moved.ChannelID == nil || *moved.ChannelID != talk {
		t.Fatal("the following occurrences moved to the Talk")
	}
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{}`, "scope=following"); err != nil {
		t.Fatalf("an empty edit answers with the event: %v", err)
	}
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{"status":"cancelled"}`, "scope=following"); err != nil {
		t.Fatal(err)
	}
	if got := f.occurrences(t, first.Series.ID); got[0].Status == StatusCancelled || got[1].Status != StatusCancelled || got[4].Status != StatusCancelled {
		t.Fatalf("statuses: %+v", got)
	}
}

// newTalk creates a voice channel everybody may view.
func (f *rosterFixture) newTalk(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ('open-talk', 'voice') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	f.h.Perms.Invalidate()
	return id
}

func TestAnEventOfATalkSeriesAuditsWithItsChannel(t *testing.T) {
	f := newRosterFixture(t)
	talk := f.newTalk(t)
	first := f.startSeries(t, fmt.Sprintf(`{"title":"Talk night","description":"","start_time":%%q,"channel_id":%q,"recurrence":{"frequency":"monthly"}}`, talk))
	f.event = first
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{"title":"Talk night II"}`, "scope=following"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = 'event.talk_edit' AND details->>'scope' = 'following'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audited %d times: %v", n, err)
	}
}

func TestSeriesStoreRefusesWhatDoesNotExistOrIsInvalid(t *testing.T) {
	f := newRosterFixture(t)
	ctx := context.Background()
	ghost := &CommunityEvent{ID: uuid.New(), Series: &SeriesInfo{ID: uuid.New()}}
	title := "x"
	if _, err := f.s.UpdateFollowing(ctx, ghost, UpdateEventRequest{Title: &title}); err == nil {
		t.Fatal("an unknown series is not found")
	}
	if _, err := f.s.CancelFollowing(ctx, ghost); err == nil {
		t.Fatal("an unknown series is not found")
	}
	if _, _, err := f.s.SetSeriesRoles(ctx, ghost, nil); err == nil {
		t.Fatal("an unknown series is not found")
	}
	if _, _, err := f.s.SetSeriesRoles(ctx, ghost, []RoleInput{{Name: "A"}, {Name: "a"}}); err == nil {
		t.Fatal("two roles with one name are refused before anything is stored")
	}
	if _, err := f.s.GenerateSeries(ctx, uuid.New(), time.Now()); err == nil {
		t.Fatal("generating an unknown series fails")
	}
	if _, _, err := f.s.CreateSeries(ctx, CreateEventRequest{Title: "x", StartTime: time.Now().Add(time.Hour), Location: ptr("Hall")}, RecurrenceRequest{Frequency: FreqWeekly, Timezone: "Mars/Base"}, f.creator.ID); err == nil {
		t.Fatal("an unknown zone is refused")
	}
}

func TestMovingAFollowingOccurrenceMovesItsCoverAlong(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	list := f.occurrences(t, first.Series.ID)
	ctx := context.Background()
	var media uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO media (uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes) VALUES ($1, 'b', 'k', 'c.png', 'image/png', 1) RETURNING id`, f.owner.ID).Scan(&media); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE events SET cover_media_id = $2 WHERE id = $1`, list[1].ID, media); err != nil {
		t.Fatal(err)
	}
	talk := f.newTalk(t)
	f.event = &CommunityEvent{ID: list[0].ID}
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, fmt.Sprintf(`{"channel_id":%q}`, talk), "scope=following"); err != nil {
		t.Fatal(err)
	}
	var channel *uuid.UUID
	if err := f.p.QueryRow(ctx, `SELECT channel_id FROM media WHERE id = $1`, media).Scan(&channel); err != nil || channel == nil || *channel != talk {
		t.Fatalf("the cover follows the occurrence to its Talk: %v %v", channel, err)
	}
}

// A form that always sends the start must not re-anchor a series: a monthly series on the 31st, edited at its clamped February occurrence, stays on the 31st.
func TestAFollowingEditWithTheStartUnchangedKeepsTheAnchor(t *testing.T) {
	f := newRosterFixture(t)
	ctx := context.Background()
	jan31 := time.Date(2030, 1, 31, 12, 0, 0, 0, time.UTC)
	ev, seriesID, err := f.s.CreateSeries(ctx, CreateEventRequest{Title: "Monthly", StartTime: jan31, Location: ptr("Hall")}, RecurrenceRequest{Frequency: FreqMonthly}, f.creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GenerateSeries(ctx, seriesID, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	list := f.occurrences(t, seriesID)
	if len(list) != 2 || list[1].Start.Day() != 28 {
		t.Fatalf("january and the clamped february: %+v", list)
	}
	f.event = &CommunityEvent{ID: list[1].ID}
	body := fmt.Sprintf(`{"title":"Monthly II","start_time":%q}`, list[1].Start.Format(time.RFC3339))
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, body, "scope=following"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GenerateSeries(ctx, seriesID, time.Date(2030, 3, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got := f.occurrences(t, seriesID)
	if len(got) < 3 || got[2].Start.Day() != 31 || got[2].Title != "Monthly II" {
		t.Fatalf("march is still the 31st and carries the new title: %+v", got)
	}
	_ = ev
}

// What was changed by hand on one occurrence survives an edit that does not touch it.
func TestAFollowingEditLeavesIndividuallyChangedFieldsAlone(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	list := f.occurrences(t, first.Series.ID)
	f.event = &CommunityEvent{ID: list[2].ID}
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{"title":"Special","location":"Rooftop"}`, ""); err != nil {
		t.Fatal(err)
	}
	f.event = &CommunityEvent{ID: list[1].ID}
	if _, err := f.callQuery(t, f.owner, f.h.updateEvent, http.MethodPatch, `{"description":"for everyone"}`, "scope=following"); err != nil {
		t.Fatal(err)
	}
	special, _ := f.s.Get(context.Background(), list[2].ID, nil)
	if special.Title != "Special" || special.Location == nil || *special.Location != "Rooftop" || special.Description != "for everyone" {
		t.Fatalf("the hand-made title and place stay, the description follows: %+v", special)
	}
	if !special.StartTime.Equal(list[2].Start) {
		t.Fatal("and so does the time")
	}
}

func TestCancellingNothingDoesNotEndTheSeries(t *testing.T) {
	f := newRosterFixture(t)
	first := f.startSeries(t, weeklyBody)
	list := f.occurrences(t, first.Series.ID)
	ctx := context.Background()
	if _, err := f.p.Exec(ctx, `UPDATE events SET status = 'completed' WHERE series_id = $1 AND series_index >= 3`, first.Series.ID); err != nil {
		t.Fatal(err)
	}
	ids, err := f.s.CancelFollowing(ctx, &CommunityEvent{ID: list[3].ID, Series: &SeriesInfo{ID: first.Series.ID, Index: 3}})
	if err != nil || len(ids) != 0 {
		t.Fatalf("nothing was open: %v %v", ids, err)
	}
	var status string
	if err := f.p.QueryRow(ctx, `SELECT status FROM event_series WHERE id = $1`, first.Series.ID).Scan(&status); err != nil || status != "active" {
		t.Fatalf("the series goes on: %q %v", status, err)
	}
	f.event = &CommunityEvent{ID: list[3].ID}
	if _, err := f.callQuery(t, f.owner, f.h.cancelEvent, http.MethodPost, ``, "scope=following"); err == nil {
		t.Fatal("the handler answers with a conflict")
	}
}
