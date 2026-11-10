package readstate

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// Without a permission service the request fails: no read state is taken to
// be visible.
func TestVisibleReadStatesFailWithoutPermissions(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	states := []ReadState{{ChannelID: uuid.New()}}
	if _, err := visibleReadStates(core.Guard{}, r, states); err == nil {
		t.Fatal("read states were listed without a permission service")
	}
}
