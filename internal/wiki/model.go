// Package wiki is the community's wiki (doc/wiki.md): one tree of pages with a
// revision for every save, read by everyone who may view the wiki and written
// by the roles that were given EditWiki.
package wiki

import (
	"time"

	"github.com/google/uuid"
)

// Limits. They are part of the contract (doc/wiki.md) and checked again in the database where it can.
const (
	// MaxContentBytes is the longest page text.
	MaxContentBytes = 50 * 1024
	// MaxTitleRunes is the longest page title.
	MaxTitleRunes = 100
	// MaxSummaryRunes is the longest edit summary.
	MaxSummaryRunes = 200
	// MaxSlugLen is the longest page address.
	MaxSlugLen = 80
	// MaxPages is how many pages the wiki holds (deleted ones do not count).
	MaxPages = 500
	// MaxDepth is how many levels the tree has: a page at the top is level 1.
	MaxDepth = 5
	// KeepRevisions is how many of the latest revisions of a page are kept;
	// older ones are pruned, except those a kept restore points to.
	KeepRevisions = 100
	// EditsPerMinute caps the writes of one member (saves, restores, moves,
	// locks and deletes).
	EditsPerMinute = 20
	// SearchLimit is the most results of a search.
	SearchLimit = 30
	// RevisionPage is the page size of the revision list.
	RevisionPage = 50
)

// UserRef names a member in short.
type UserRef struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
}

// PageSummary is a page without its text: what the tree shows.
type PageSummary struct {
	ID        uuid.UUID  `json:"id" format:"uuid"`
	Slug      string     `json:"slug"`
	Title     string     `json:"title"`
	ParentID  *uuid.UUID `json:"parent_id" format:"uuid" extensions:"x-nullable"`
	Position  int        `json:"position"`
	Locked    bool       `json:"locked"`
	UpdatedAt time.Time  `json:"updated_at" format:"date-time"`
	// DeletedAt is set on a deleted page, which only holders of Manage wiki see.
	DeletedAt *time.Time `json:"deleted_at,omitempty" format:"date-time" binding:"optional"`
}

// Page is a page with its text.
type Page struct {
	PageSummary
	Content string `json:"content"`
	// Rev is the latest revision; an edit names it as base_rev.
	Rev       int       `json:"rev"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	CreatedBy *UserRef  `json:"created_by,omitempty" binding:"optional"`
	UpdatedBy *UserRef  `json:"updated_by,omitempty" binding:"optional"`
	// RedirectedFrom is the old address the request used, when the page was renamed.
	RedirectedFrom string `json:"redirected_from,omitempty" binding:"optional"`
}

// Revision is one saved state of a page; Content and Title are only in the single-revision answer.
type Revision struct {
	Rev          int       `json:"rev"`
	Editor       *UserRef  `json:"editor,omitempty" binding:"optional"`
	Summary      string    `json:"summary"`
	RestoredFrom *int      `json:"restored_from,omitempty" binding:"optional"`
	Size         int       `json:"size"`
	CreatedAt    time.Time `json:"created_at" format:"date-time"`
	Title        string    `json:"title,omitempty" binding:"optional"`
	Content      *string   `json:"content,omitempty" binding:"optional"`
}

// SearchHit is a page that matches a search, with a few words around the match.
type SearchHit struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// PageInput is the body of POST /api/wiki/pages.
type PageInput struct {
	Title string `json:"title" minLength:"1" maxLength:"100"`
	// Slug is the page's address; empty derives it from the title.
	Slug     string     `json:"slug,omitempty" binding:"optional" maxLength:"80"`
	ParentID *uuid.UUID `json:"parent_id,omitempty" binding:"optional" format:"uuid"`
	Content  string     `json:"content"`
	Summary  string     `json:"summary,omitempty" binding:"optional" maxLength:"200"`
}

// PageChange is the body of PATCH /api/wiki/pages/{slug}: a field that is
// absent stays as it is. BaseRev says which revision the change was made on.
type PageChange struct {
	BaseRev int     `json:"base_rev" minimum:"1"`
	Title   *string `json:"title,omitempty" binding:"optional" maxLength:"100"`
	Content *string `json:"content,omitempty" binding:"optional"`
	Summary string  `json:"summary,omitempty" binding:"optional" maxLength:"200"`
	// Slug, ParentID (the zero UUID moves the page to the top) and Position
	// are the page tree's and need Manage wiki.
	Slug     *string    `json:"slug,omitempty" binding:"optional" maxLength:"80"`
	ParentID *uuid.UUID `json:"parent_id,omitempty" binding:"optional" format:"uuid"`
	Position *int       `json:"position,omitempty" binding:"optional" minimum:"0"`
}

// RestoreInput is the body of the revision restore.
type RestoreInput struct {
	BaseRev int    `json:"base_rev" minimum:"1"`
	Summary string `json:"summary,omitempty" binding:"optional" maxLength:"200"`
}

// LockInput is the body of the lock route.
type LockInput struct {
	Locked bool `json:"locked"`
}
