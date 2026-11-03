package userdata

import (
	"context"
	"io"
	"sort"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// Channels is the set of channels whose names an export may include.
// All is set for the Owner and for Administrators, who can see every channel.
type Channels struct {
	All bool
	IDs map[uuid.UUID]bool
}

// Has reports whether the channel's name may appear.
func (c Channels) Has(id uuid.UUID) bool { return c.All || c.IDs[id] }

// Limits are the export bounds. Auth reads them when an export starts, so a
// test that lowers the variables is what the builders see.
type Limits struct {
	Page         int
	MaxRows      int
	MaxMessages  int
	MessageBytes int
	MaxEdits     int
}

// File is one entry of the archive, written in Order. Build, when set, returns
// the JSON value and whether the list was cut. Stream, when set, writes the
// file itself; messages.json is streamed a page at a time and is not buffered.
type File struct {
	Name   string
	Order  int
	Build  func() (value any, truncated bool, err error)
	Stream func(io.Writer) error
}

// Arg selects the arguments of an erasure statement.
type Arg int

const (
	// ArgID passes the account id as $1.
	ArgID Arg = iota
	// ArgUsername passes the account's current username as $1.
	ArgUsername
	// ArgIDAndName passes the account id and its tombstone username.
	ArgIDAndName
	// ArgNone passes no arguments.
	ArgNone
)

// Step is one erasure statement. Order is its place in the original sequence.
// Keys means the statement returns s3 keys to remove after the commit.
type Step struct {
	Order int
	SQL   string
	Arg   Arg
	Keys  bool
}

// Export contributes one domain's archive files. The builders run later, one
// file at a time, so Export itself must not read the rows.
type Export interface {
	Export(ctx context.Context, q db.Querier, user uuid.UUID, channels Channels, limits Limits) ([]File, error)
}

// Erase contributes one domain's erasure statements. It does not run them.
type Erase interface {
	Erase() []Step
}

// Registry is the list of domain contributors. Register it before serving.
type Registry struct {
	exports []Export
	erases  []Erase
}

// Register adds a domain. A nil side is skipped.
func (r *Registry) Register(exp Export, era Erase) {
	if r == nil {
		return
	}
	if exp != nil {
		r.exports = append(r.exports, exp)
	}
	if era != nil {
		r.erases = append(r.erases, era)
	}
}

// Files returns every domain file, sorted by Order. Builders have not run.
func (r *Registry) Files(ctx context.Context, q db.Querier, user uuid.UUID, channels Channels, limits Limits) ([]File, error) {
	if r == nil {
		return nil, nil
	}
	var out []File
	for _, exp := range r.exports {
		files, err := exp.Export(ctx, q, user, channels, limits)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
	}
	SortFiles(out)
	return out, nil
}

// Steps returns every domain statement, sorted by Order.
func (r *Registry) Steps() []Step {
	if r == nil {
		return nil
	}
	var out []Step
	for _, era := range r.erases {
		out = append(out, era.Erase()...)
	}
	SortSteps(out)
	return out
}

// SortFiles sorts archive entries by Order. Equal orders keep their order.
func SortFiles(files []File) {
	sort.SliceStable(files, func(i, j int) bool { return files[i].Order < files[j].Order })
}

// SortSteps sorts erasure statements by Order. Equal orders keep their order.
func SortSteps(steps []Step) {
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Order < steps[j].Order })
}
