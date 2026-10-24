package communityevents

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateEventText(t *testing.T) {
	if err := validateEventText("", "desc"); err == nil {
		t.Fatal("expected error on empty title")
	}
	if err := validateEventText("   ", "desc"); err == nil {
		t.Fatal("expected error on whitespace title")
	}
	longTitle := string(make([]rune, 101))
	if err := validateEventText(longTitle, "desc"); err == nil {
		t.Fatal("expected error on title > 100 runes")
	}
	longDesc := string(make([]rune, 2001))
	if err := validateEventText("Valid Title", longDesc); err == nil {
		t.Fatal("expected error on desc > 2000 runes")
	}
	if err := validateEventText("Valid Title", "Valid description"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEventTimes(t *testing.T) {
	if err := validateEventTimes(time.Time{}, nil); err == nil {
		t.Fatal("expected error on zero start time")
	}
	start := time.Now()
	before := start.Add(-1 * time.Hour)
	if err := validateEventTimes(start, &before); err == nil {
		t.Fatal("expected error when end_time is before start_time")
	}
	after := start.Add(1 * time.Hour)
	if err := validateEventTimes(start, &after); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEventLocationBasics(t *testing.T) {
	h := &Handler{}
	// Neither channel nor location
	if err := h.validateEventLocation(nil, nil, nil); err == nil {
		t.Fatal("expected error when neither channel nor location is set")
	}
	emptyLoc := "  "
	if err := h.validateEventLocation(nil, nil, &emptyLoc); err == nil {
		t.Fatal("expected error when location is whitespace only")
	}
	// Both channel and location
	chID := uuid.New()
	loc := "Auditorium"
	if err := h.validateEventLocation(nil, &chID, &loc); err == nil {
		t.Fatal("expected error when both channel and location are set")
	}
	// Free text location > 255 chars
	longLoc := string(make([]rune, 256))
	if err := h.validateEventLocation(nil, nil, &longLoc); err == nil {
		t.Fatal("expected error on location > 255 runes")
	}
	// Valid free text location
	if err := h.validateEventLocation(nil, nil, &loc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
