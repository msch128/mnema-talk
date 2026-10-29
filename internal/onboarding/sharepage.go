package onboarding

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/mdplain"
)

// maxShareDescription is the longest description a link preview of an invite
// shows; messengers cut at about this length anyway.
const maxShareDescription = 200

// ShareInfo is the public face of the community for a link preview of the
// instance root or of an invite: what a messenger shows when someone pastes
// the link. It holds only what the invite preview already tells anyone who has
// a working code (the name) plus the welcome text, which is written for exactly
// the people an invite brings in. Nothing from inside the community.
type ShareInfo struct {
	CommunityName string
	// Description is the welcome text as one line of plain text, cut short.
	Description string
}

// RootShareInfo is the community's name for the preview of the root URL. The
// welcome text is not part of it: without an invite it is not for strangers.
func (h *Handler) RootShareInfo(ctx context.Context) (ShareInfo, error) {
	var s ShareInfo
	err := h.DB.QueryRow(ctx, `SELECT community_name FROM instance_settings`).Scan(&s.CommunityName)
	return s, err
}

// InviteShareInfo is what the preview of an invite link shows. ok is false for
// a code that cannot be used (unknown, expired, used up, revoked), and also
// when the caller is over the invite preview budget: both get the generic
// page, so the answer reveals no more than GET /api/invites/{code} does.
func (h *Handler) InviteShareInfo(r *http.Request, code string) (info ShareInfo, ok bool, err error) {
	if !previewCodePattern.MatchString(code) {
		return ShareInfo{}, false, nil
	}
	if allowed, _ := h.previewPerIP.Allow(httpx.ClientIPKey(r)); !allowed {
		return ShareInfo{}, false, nil
	}
	if allowed, _ := h.previewPerSite.Allow(httpx.ClientAggregateKey(r)); !allowed {
		return ShareInfo{}, false, nil
	}
	row, err := h.lookupInvite(r.Context(), code)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShareInfo{}, false, nil
	}
	if err != nil {
		return ShareInfo{}, false, err
	}
	return ShareInfo{CommunityName: row.communityName, Description: plainSnippet(row.welcomeText, maxShareDescription)}, true, nil
}

// plainSnippet turns Markdown into one line of plain text of at most limit
// runes, cut at a word and marked with an ellipsis.
func plainSnippet(markdown string, limit int) string {
	s := strings.Join(strings.Fields(mdplain.Plain(markdown, "…")), " ")
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	cut := []rune(s)[:limit-1]
	if i := strings.LastIndexByte(string(cut), ' '); i > limit/2 {
		return strings.TrimRight(string(cut)[:i], " ,.;:-") + "…"
	}
	return strings.TrimRight(string(cut), " ,.;:-") + "…"
}
