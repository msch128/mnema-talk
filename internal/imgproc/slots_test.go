package imgproc

import (
	"errors"
	"testing"
	"time"
)

func fillSlots(t *testing.T) {
	t.Helper()
	n := cap(slots)
	for i := 0; i < n; i++ {
		slots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < n; i++ {
			<-slots
		}
	})
}

func TestRedrawGivesUpWhenEverySlotIsTaken(t *testing.T) {
	old := maxWait
	maxWait = 20 * time.Millisecond
	t.Cleanup(func() { maxWait = old })
	fillSlots(t)
	if _, err := ProcessAvatar(nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want busy", err)
	}
}

func TestGateHandsTheSlotBack(t *testing.T) {
	fillSlots(t)
	<-slots
	release, err := Gate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
	slots <- struct{}{}
}
