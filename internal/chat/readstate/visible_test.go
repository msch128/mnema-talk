package readstate

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// Without a permission service the read states of every channel are kept.
func TestVisibleReadStatesWithoutPermissionsLimitsNothing(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	states := []ReadState{{ChannelID: uuid.New()}}
	if got, err := visibleReadStates(core.Guard{}, r, states); err != nil || len(got) != 1 {
		t.Fatalf("read states: %v %v", got, err)
	}
}
