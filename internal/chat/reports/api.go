package reports

import (
	"github.com/mnema-xyz/talk/internal/audit"
	"github.com/mnema-xyz/talk/internal/chat/core"
	"github.com/mnema-xyz/talk/internal/db"
	"github.com/mnema-xyz/talk/internal/events"
	"github.com/mnema-xyz/talk/internal/httpx"
)

// API serves message reports: filing one and the moderation queue. The chat
// Handler builds it from its own fields, so the server wiring is unchanged.
type API struct {
	DB *db.Pool
	// Guard decides who may see the reported message's channel and who holds
	// Manage reports.
	Guard core.Guard
	// Audit records resolved reports; nil records nothing.
	Audit *audit.Log
	// ReportLimit caps message reports per member; nil means no cap.
	ReportLimit *httpx.RateLimiter
	// Events tells moderators the open count changed; nil tells nobody.
	Events events.Publisher
}
