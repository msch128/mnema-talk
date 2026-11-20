package core

import "testing"

// LiveMessages is a constant for the queries built from it; it must say what
// LiveSQL says, the thread root included.
func TestLiveMessagesSpellsOutLiveSQL(t *testing.T) {
	want := "(SELECT * FROM messages live WHERE " + LiveSQL("live") + ")"
	if LiveMessages != want {
		t.Fatalf("LiveMessages = %q\nwant %q", LiveMessages, want)
	}
}

func TestLiveMSpellsOutLiveSQL(t *testing.T) {
	if LiveM != LiveSQL("m") {
		t.Fatalf("LiveM = %q\nwant %q", LiveM, LiveSQL("m"))
	}
}
