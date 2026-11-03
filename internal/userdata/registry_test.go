package userdata

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

func TestRegistrySortsFilesAndSteps(t *testing.T) {
	r := &Registry{}
	r.Register(ordered{order: 30, name: "later", sql: "second"}, ordered{order: 2, name: "later", sql: "second"})
	r.Register(ordered{order: 10, name: "first", sql: "first"}, ordered{order: 1, name: "first", sql: "first"})
	files, err := r.Files(context.Background(), nil, uuid.Nil, Channels{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Name != "first" || files[1].Name != "later" {
		t.Fatalf("files: %v", fileNames(files))
	}
	steps := r.Steps()
	if len(steps) != 2 || steps[0].SQL != "first" || steps[1].SQL != "second" {
		t.Fatalf("steps: %+v", steps)
	}
}

func TestNilRegistryContributesNothing(t *testing.T) {
	var r *Registry
	r.Register(ordered{}, ordered{})
	files, err := r.Files(context.Background(), nil, uuid.Nil, Channels{}, Limits{})
	if err != nil || files != nil {
		t.Fatalf("files %v %v", files, err)
	}
	if r.Steps() != nil {
		t.Fatal("steps")
	}
}

func TestChannelsHideWhatTheMemberCannotSee(t *testing.T) {
	id := uuid.New()
	if (Channels{}).Has(id) {
		t.Fatal("empty set")
	}
	if !(Channels{All: true}).Has(id) {
		t.Fatal("all")
	}
	if !(Channels{IDs: map[uuid.UUID]bool{id: true}}).Has(id) {
		t.Fatal("listed")
	}
}

// ordered is one file and one statement, so a test can register it as both.
type ordered struct {
	order int
	name  string
	sql   string
}

func (o ordered) Export(context.Context, db.Querier, uuid.UUID, Channels, Limits) ([]File, error) {
	return []File{{Name: o.name, Order: o.order}}, nil
}

func (o ordered) Erase() []Step {
	return []Step{{Order: o.order, SQL: o.sql}}
}

func fileNames(files []File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Name
	}
	return out
}
