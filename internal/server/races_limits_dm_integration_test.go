//go:build integration

package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Lane 7 of the defect hunt, third batch: caps of conversations and of
// scheduled messages, with a fresh actor per round (the per-member rates).

// Ten new conversations a day: with nine started, three starts at once (the
// per-minute start rate) leave ten.
func TestParallelConversationStartsStayWithinTheDailyCap(t *testing.T) {
	f := newDMFixture(t, false)
	const callers = 3 // dm.NewDMsPerMinute
	var starter *client
	others := make([]*client, callers)
	racetest.Sweep(t, racetest.Rounds(10, 100), callers,
		func(round int) {
			starter = f.a.sqlMember(fmt.Sprintf("starter%d", round))
			for i := range others {
				others[i] = f.a.sqlMember(fmt.Sprintf("other%d-%d", round, i))
			}
			f.a.runSQL(`WITH c AS (INSERT INTO channels (number, name, type, dm_key, dm_created_by) OVERRIDING SYSTEM VALUE
				SELECT nextval('dm_number_seq'), '', 'dm', gen_random_uuid()::text, $1 FROM generate_series(1, $2) RETURNING id)
				INSERT INTO dm_members (channel_id, user_id) SELECT id, $1 FROM c`, starter.user.ID, dm.MaxNewDMsPerDay-1)
		},
		func(_, i int) error {
			return statusErr(starter.startDM(others[i]), http.StatusCreated, http.StatusTooManyRequests)
		},
		strict(func(round int, _ racetest.Round) string {
			n := f.a.count(`SELECT COUNT(*) FROM channels WHERE type = 'dm' AND dm_created_by = $1`, starter.user.ID)
			if n != dm.MaxNewDMsPerDay {
				return fmt.Sprintf("round %d: %d conversations started today", round, n)
			}
			return ""
		})).FailOnViolation(t)
}

// A group conversation holds ten members: with nine in it, members add
// different people at once. Expected failure until H-L7-06 is fixed: each
// add counts the members in the statement that locks the conversation row,
// and a waiter's count is the one of its snapshot from before the wait.
func TestParallelAddsStayWithinTheGroupSize(t *testing.T) {
	for _, callers := range []int{2, raceCallers} {
		t.Run(fmt.Sprint(callers, " adders"), func(t *testing.T) {
			sweepGroupAdds(t, callers).KnownFailure(t, "H-L7-06")
		})
	}
}

func sweepGroupAdds(t *testing.T, callers int) racetest.Result {
	f := newDMFixture(t, false)
	var group uuid.UUID
	var members, outsiders []*client
	return racetest.Sweep(t, racetest.Rounds(10, 100), callers,
		func(round int) {
			members, outsiders = nil, nil
			for i := range dm.MaxDMMembers - 1 {
				members = append(members, f.a.sqlMember(fmt.Sprintf("in%d-%d", round, i)))
			}
			for i := range callers {
				outsiders = append(outsiders, f.a.sqlMember(fmt.Sprintf("out%d-%d", round, i)))
			}
			var view dmView
			members[0].startDM(members[1:]...).decode(t, &view)
			group = view.ID
		},
		func(_, i int) error {
			adder := members[i%len(members)]
			r := adder.post("/api/dms/"+group.String()+"/members", map[string]any{"user_id": outsiders[i].user.ID})
			return statusErr(r, http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusConflict)
		},
		strict(func(round int, _ racetest.Round) string {
			if n := f.a.count(`SELECT COUNT(*) FROM dm_members WHERE channel_id = $1`, group); n != dm.MaxDMMembers {
				return fmt.Sprintf("round %d: %d members", round, n)
			}
			return ""
		}))
}

// Unsent files per member: with one short of the cap, uploads at once.
func TestParallelUploadsStayWithinTheStagedCap(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "files", "text")
	const maxStaged = 30 // maxStagedPerMember in internal/media
	var member *client
	racetest.Sweep(t, racetest.Rounds(5, 100), raceCallers,
		func(round int) {
			member = a.sqlMember(fmt.Sprintf("uploader%d", round))
			a.runSQL(`INSERT INTO media (uploader_id, channel_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes, staged)
				SELECT $1, $2, 'b', 'filler/' || gen_random_uuid(), 'f.png', 'image/png', 1, TRUE FROM generate_series(1, $3)`, member.user.ID, ch, maxStaged-1)
		},
		func(_, i int) error {
			r := member.upload("/api/channels/"+ch.String()+"/attachments", "file", fmt.Sprintf("r%d.png", i), "image/png", pngBytes, nil)
			return statusErr(r, http.StatusCreated, http.StatusBadRequest)
		},
		strict(func(round int, _ racetest.Round) string {
			if n := a.count(`SELECT COUNT(*) FROM media WHERE uploader_id = $1 AND staged`, member.user.ID); n != maxStaged {
				return fmt.Sprintf("round %d: %d unsent files", round, n)
			}
			return ""
		})).FailOnViolation(t)
}

func TestParallelSchedulesStayWithinTheCap(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	var member *client
	racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
		func(round int) {
			member = a.sqlMember(fmt.Sprintf("scheduler%d", round))
			a.runSQL(`INSERT INTO scheduled_messages (user_id, channel_id, content, send_at)
				SELECT $1, $2, 'x', NOW() + INTERVAL '1 day' FROM generate_series(1, $3)`, member.user.ID, ch, later.MaxScheduled-1)
		},
		func(_, i int) error {
			at := time.Now().Add(time.Duration(i+2) * time.Hour).UTC().Format(time.RFC3339)
			r := member.post("/api/channels/"+ch.String()+"/scheduled", map[string]any{"content": fmt.Sprintf("later %d", i), "send_at": at})
			return statusErr(r, http.StatusCreated, http.StatusConflict)
		},
		strict(func(round int, _ racetest.Round) string {
			if n := a.count(`SELECT COUNT(*) FROM scheduled_messages WHERE user_id = $1`, member.user.ID); n != later.MaxScheduled {
				return fmt.Sprintf("round %d: %d scheduled", round, n)
			}
			return ""
		})).FailOnViolation(t)
}
