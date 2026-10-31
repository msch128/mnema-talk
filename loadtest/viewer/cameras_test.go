package main

import "testing"

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = string(rune('a' + i))
	}
	return out
}

func TestParseCameraView(t *testing.T) {
	for in, want := range map[string]int{"": -1, "all": -1, "TRUE": -1, "yes": -1, "none": 0, "0": 0, "3": 3} {
		got, err := parseCameraView(in)
		if err != nil || got.limit != want {
			t.Errorf("%q: %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"-1", "many", "1.5"} {
		if _, err := parseCameraView(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestPlanHidesTheOthersAndNeverCountsSelf(t *testing.T) {
	cams := ids(5)
	v := cameraView{limit: 2}
	p := v.plan("a", 0, cams) // others: b c d e
	if p.allOff || len(p.hide) != 2 {
		t.Fatalf("plan = %+v", p)
	}
	for _, h := range p.hide {
		if h == "a" {
			t.Fatal("hides itself")
		}
	}
	if p := allCameras.plan("a", 0, cams); p.allOff || p.hide != nil {
		t.Fatalf("all cameras: %+v", p)
	}
	if p := (cameraView{}).plan("a", 0, cams); !p.allOff {
		t.Fatalf("none: %+v", p)
	}
	if p := (cameraView{limit: 9}).plan("a", 0, cams); p.hide != nil {
		t.Fatalf("a limit above the others hides: %+v", p)
	}
}

func TestWatchersAddUpToMembersTimesLimit(t *testing.T) {
	cams := ids(10)
	members := ids(30) // the first ten publish
	v := cameraView{limit: 4}
	total := 0
	for _, n := range v.watchers(members, cams) {
		total += n
	}
	if total != 30*4 {
		t.Fatalf("shown cameras = %d, want %d", total, 30*4)
	}
	// Spread: no camera is shown to everybody while others are shown to nobody.
	for id, n := range v.watchers(members, cams) {
		if n == 0 || n > 20 {
			t.Errorf("camera %s shown to %d members", id, n)
		}
	}
	all := allCameras.watchers(members, cams)
	if all["a"] != 29 || all["j"] != 29 {
		t.Fatalf("all cameras: %v", all)
	}
	if got := (cameraView{}).watchers(members, cams); len(got) != 0 {
		t.Fatalf("none: %v", got)
	}
}
