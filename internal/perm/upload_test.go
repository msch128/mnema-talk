package perm_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func ip(v int) *int { return &v }

func TestUploadMBResolution(t *testing.T) {
	user, owner := uuid.New(), uuid.New()
	small, big := Subject{ID: user}, Subject{ID: owner, Owner: true}
	f := permtest.NewFixture().Owner(owner)
	a, b := f.Role(1, 0), f.Role(2, 0)
	f.RoleUpload(a, ip(10)).RoleUpload(b, ip(30)).Assign(user, a)
	s := f.Snapshot()

	for _, tc := range []struct {
		name    string
		sub     Subject
		global  *int
		ceiling int
		want    int
	}{
		{"no setting uses the ceiling", Subject{ID: uuid.New()}, nil, 50, 50},
		{"global lowers", Subject{ID: uuid.New()}, ip(5), 50, 5},
		{"global above ceiling is capped", Subject{ID: uuid.New()}, ip(500), 50, 50},
		{"role override beats global", small, ip(5), 50, 10},
		{"role override capped by ceiling", small, ip(5), 8, 8},
		{"owner always gets the ceiling", big, ip(1), 50, 50},
	} {
		f.GlobalUpload(tc.global)
		if got := s.UploadMB(tc.sub, tc.ceiling); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}

	// Max over roles, also with @everyone's override in play.
	f.Assign(user, b).GlobalUpload(ip(5))
	if got := s.UploadMB(small, 50); got != 30 {
		t.Errorf("max over roles: %d, want 30", got)
	}
	f.RoleUpload(f.EveryoneID(), ip(40))
	if got := s.UploadMB(small, 50); got != 40 {
		t.Errorf("@everyone override joins the max: %d, want 40", got)
	}
	f.GlobalUpload(nil)
	if got := s.GlobalUploadMB(50); got != 50 {
		t.Errorf("global %d", got)
	}
}

func TestOptionalInt(t *testing.T) {
	var in RoleInput
	if err := json.Unmarshal([]byte(`{}`), &in); err != nil || in.MaxUploadMB.Present {
		t.Fatalf("absent: %+v %v", in.MaxUploadMB, err)
	}
	if err := json.Unmarshal([]byte(`{"max_upload_mb":null}`), &in); err != nil || !in.MaxUploadMB.Present || in.MaxUploadMB.Value != nil {
		t.Fatalf("null: %+v %v", in.MaxUploadMB, err)
	}
	if err := json.Unmarshal([]byte(`{"max_upload_mb":7}`), &in); err != nil || in.MaxUploadMB.Value == nil || *in.MaxUploadMB.Value != 7 {
		t.Fatalf("value: %+v %v", in.MaxUploadMB, err)
	}
	if err := json.Unmarshal([]byte(`{"max_upload_mb":"x"}`), &in); err == nil {
		t.Fatal("a string must be rejected")
	}
}

func TestUploadCeilingDefault(t *testing.T) {
	s := permtest.Unloadable()
	if s.UploadCeilingMB() != 50 {
		t.Fatalf("default ceiling %d", s.UploadCeilingMB())
	}
	s.SetUploadCeiling(128)
	if s.UploadCeilingMB() != 128 {
		t.Fatal("ceiling not applied")
	}
	if _, err := s.UploadLimitBytes(t.Context(), Subject{}); err == nil {
		t.Fatal("limit without a readable state must fail")
	}
	if _, err := s.GlobalUploadLimitBytes(t.Context()); err == nil {
		t.Fatal("global limit without a readable state must fail")
	}
	if _, err := s.UploadSettings(t.Context()); err == nil {
		t.Fatal("settings without a readable state must fail")
	}
	if _, err := s.SetGlobalUploadMB(t.Context(), Subject{Owner: true}, ip(5)); err == nil {
		t.Fatal("storing without a database must fail")
	}
	if _, err := s.SetGlobalUploadMB(t.Context(), Subject{}, nil); err == nil {
		t.Fatal("non-owner must be refused")
	}
	if _, err := s.SetGlobalUploadMB(t.Context(), Subject{Owner: true}, ip(500)); err == nil {
		t.Fatal("above the ceiling must be refused")
	}
}
