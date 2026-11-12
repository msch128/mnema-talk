//go:build integration

package server

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

// TestALayoutThatNamesAHiddenObjectAnswersLikeAMissingOneWhateverElseIsWrong
// builds random layout requests with one to three faults, at least one of them
// a hidden category or channel, and holds each to the rule of every other
// probe: with the hidden ids swapped for fresh ones the answer is the same
// bytes. The other faults (unknown ids, sort orders out of range, a mix of
// lists) are the same in both variants, so the order in which the checks run
// cannot tell the two apart.
func TestALayoutThatNamesAHiddenObjectAnswersLikeAMissingOneWhateverElseIsWrong(t *testing.T) {
	w := newCanaryWorld(t, 7)
	w.build(t)
	hidden := w.hiddenFrom("mod")
	rng := rand.New(rand.NewSource(20261112))
	const big = 2000000000
	cat := func(id string, order int) map[string]any { return map[string]any{"id": id, "sort_order": order} }
	place := func(id string, target any, order int) map[string]any {
		return map[string]any{"id": id, "category_id": target, "sort_order": order}
	}
	open := w.open.String()
	faults := []func(cats, chans *[]any){
		// The first three name a hidden object.
		func(c, _ *[]any) { *c = append(*c, cat(w.staffCat.String(), 1)) },
		func(_, h *[]any) { *h = append(*h, place(open, w.staffCat.String(), 1)) },
		func(_, h *[]any) { *h = append(*h, place(w.staffText.String(), nil, 1)) },
		func(c, _ *[]any) { *c = append(*c, cat(uuid.NewString(), 1)) },
		func(_, h *[]any) { *h = append(*h, place(uuid.NewString(), nil, 1)) },
		func(_, h *[]any) { *h = append(*h, place(open, uuid.NewString(), 1)) },
		func(c, _ *[]any) { *c = append(*c, cat(uuid.NewString(), big)) },
		func(_, h *[]any) { *h = append(*h, place(open, nil, big)) },
	}
	for i := 0; i < 300; i++ {
		var cats, chans []any
		picks := []int{rng.Intn(3)}
		for n := rng.Intn(3); n > 0; n-- {
			picks = append(picks, rng.Intn(len(faults)))
		}
		rng.Shuffle(len(picks), func(a, b int) { picks[a], picks[b] = picks[b], picks[a] })
		for _, k := range picks {
			faults[k](&cats, &chans)
		}
		body := map[string]any{}
		if len(cats) > 0 {
			body["categories"] = cats
		}
		if len(chans) > 0 {
			body["channels"] = chans
		}
		p := echoProbe{fmt.Sprintf("generated layout %d %v", i, picks), "PUT", "/api/admin/layout", body}
		p.mustMatchMissing(t, w, "mod", w.mod, hidden)
	}
}
