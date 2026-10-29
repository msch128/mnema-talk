package main

import (
	"math"
	"testing"
	"time"
)

func TestEveryConnectedSessionIsExpectedAndMissingOnesAreLoss(t *testing.T) {
	tr := newTracker()
	a := tr.newMessage(4) // 4 sessions connected
	b := tr.newMessage(4)
	for range 4 {
		tr.delivered(kindMessage, a)
	}
	for range 3 {
		tr.delivered(kindMessage, b)
	}
	tot := tr.totals(kindMessage)
	if tot.Sent != 2 || tot.Expected != 8 || tot.Got != 7 || tot.Lost != 1 || tot.Duplicates != 0 {
		t.Fatalf("totals = %+v", tot)
	}
	if got := tot.lossPct(); math.Abs(got-12.5) > 1e-9 {
		t.Fatalf("loss = %v%%, want 12.5", got)
	}
}

func TestDuplicatesAreNotLossAndDoNotHideIt(t *testing.T) {
	tr := newTracker()
	a := tr.newMessage(2)
	b := tr.newMessage(2)
	for range 3 { // message a arrives three times, one too many
		tr.delivered(kindMessage, a)
	}
	tr.delivered(kindMessage, b) // message b misses one session
	tot := tr.totals(kindMessage)
	if tot.Duplicates != 1 || tot.Lost != 1 || tot.Expected != 4 {
		t.Fatalf("totals = %+v", tot)
	}
}

func TestCancelledEventIsNeitherExpectedNorLost(t *testing.T) {
	tr := newTracker()
	seq := tr.newMessage(5)
	tr.cancel(kindMessage, seq) // the POST failed
	tot := tr.totals(kindMessage)
	if tot.Expected != 0 || tot.Lost != 0 || tot.Sent != 0 {
		t.Fatalf("totals = %+v", tot)
	}
}

func TestEditsAndReactionsAreAccountedApartFromTheMessage(t *testing.T) {
	tr := newTracker()
	seq := tr.newMessage(3)
	for range 3 {
		tr.delivered(kindMessage, seq)
	}
	tr.begin(kindEdit, seq, 3)
	tr.delivered(kindEdit, seq)
	tr.begin(kindReaction, seq, 3)
	for range 3 {
		tr.delivered(kindReaction, seq)
	}
	if tot := tr.totals(kindMessage); tot.Lost != 0 {
		t.Fatalf("message totals = %+v", tot)
	}
	if tot := tr.totals(kindEdit); tot.Expected != 3 || tot.Lost != 2 {
		t.Fatalf("edit totals = %+v", tot)
	}
	if tot := tr.totals(kindReaction); tot.Lost != 0 || tot.Got != 3 {
		t.Fatalf("reaction totals = %+v", tot)
	}
}

func TestUnknownOrNotStartedEventsAreRefused(t *testing.T) {
	tr := newTracker()
	seq := tr.newMessage(1)
	if tr.delivered(kindMessage, seq+7) {
		t.Fatal("an unknown sequence number was accepted")
	}
	if tr.delivered(kindEdit, seq) {
		t.Fatal("an edit nobody started was accepted")
	}
	if tr.delivered(kindMessage, -1) {
		t.Fatal("a negative sequence number was accepted")
	}
}

func TestLatencyIsMeasuredFromTheStartOfTheRequest(t *testing.T) {
	tr := newTracker()
	seq := tr.newMessage(1)
	time.Sleep(30 * time.Millisecond)
	tr.delivered(kindMessage, seq)
	sum := tr.lat[kindMessage].Summarize()
	if sum.Count != 1 || sum.P50Ms < 30 || sum.P50Ms > 500 {
		t.Fatalf("latency = %+v", sum)
	}
}
