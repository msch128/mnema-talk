package readstate

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// visibleReadStates keeps the read states of viewable channels.
func visibleReadStates(g core.Guard, r *http.Request, states []ReadState) ([]ReadState, error) {
	visible, err := g.ViewableChannels(r)
	if err != nil {
		return states, err
	}
	if visible == nil {
		return states, nil
	}
	ok := make(map[uuid.UUID]bool, len(visible))
	for _, id := range visible {
		ok[id] = true
	}
	out := make([]ReadState, 0, len(states))
	for _, s := range states {
		if ok[s.ChannelID] {
			out = append(out, s)
		}
	}
	return out, nil
}
