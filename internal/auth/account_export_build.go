package auth

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

const (
	// The archive is streamed to object storage while it is made, so memory
	// stays at one page of rows whatever the size of the export. Three bounds
	// apply: messages stop at exportMaxMessages or exportMessageBytes of JSON
	// (the archive says so in "truncated"), the other lists at exportMaxRows,
	// and the compressed archive at exportMaxBytes, which fails the export.
	exportMaxRows = 50_000
	exportPage    = 1000
)

// The bounds are variables so tests can lower them.
var (
	exportMaxMessages  = 200_000
	exportMessageBytes = 48 << 20
	exportMaxBytes     = int64(64 << 20)
)

var errExportTooLarge = errors.New("export too large")

// budgetWriter passes bytes on and refuses to pass more than max.
type budgetWriter struct {
	w   io.Writer
	n   int64
	max int64
}

func (b *budgetWriter) Write(p []byte) (int, error) {
	if b.n+int64(len(p)) > b.max {
		return 0, errExportTooLarge
	}
	n, err := b.w.Write(p)
	b.n += int64(n)
	return n, err
}

const exportReadme = `Your data from Mnema Talk (GDPR Art. 20).

profile.json    your account: names, bio, status, language, role, e-mail address, settings
settings.json   notification levels per channel and category
messages.json   the messages you wrote (channel_name is empty for channels you cannot see any more)
edits.json      the earlier versions of the messages you edited (newest first)
reactions.json  the reactions you gave
poll_votes.json the options you voted for in polls
uploads.json    the files you uploaded, with links while they still exist
audit.json      administrative log entries about your account (no other person's name)
security.json   what is set up for signing in (no secrets)

Other members' personal data is not part of this archive.
This file was available for download once, for 24 hours.
`

// buildExport streams the archive into object storage and marks the export
// ready. A delete of the account while it runs wins: the file is removed
// again. Nothing is buffered beyond one page of rows.
func (h *Handler) buildExport(ctx context.Context, id, user uuid.UUID) error {
	key := fmt.Sprintf("exports/%s/%s.zip", user, id)
	pr, pw := io.Pipe()
	counted := &budgetWriter{w: pw, max: exportMaxBytes}
	written := make(chan error, 1)
	go func() {
		err := h.writeArchive(ctx, user, counted)
		_ = pw.CloseWithError(err)
		written <- err
	}()
	uploadErr := h.Objects.Upload(ctx, key, pr, "application/zip", -1)
	_ = pr.CloseWithError(uploadErr) // frees the writer if the upload stopped early
	err := <-written
	if err == nil {
		err = uploadErr
	}
	if err != nil {
		_ = h.Objects.Delete(context.WithoutCancel(ctx), key)
		return fmt.Errorf("export: %w", err)
	}
	tag, err := h.Sessions.DB.Exec(ctx, `UPDATE data_exports SET status = 'ready', s3_key = $2, size_bytes = $3,
		ready_at = NOW(), expires_at = NOW() + $4 * INTERVAL '1 second' WHERE id = $1 AND status = 'pending'`,
		id, key, counted.n, int64(exportTTL.Seconds()))
	if err == nil && tag.RowsAffected() == 1 {
		return nil
	}
	_ = h.Objects.Delete(context.WithoutCancel(ctx), key)
	return err
}

type zipFiles struct{ zw *zip.Writer }

func (z *zipFiles) add(name string, v any) error {
	w, err := z.zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// writeArchive collects the person's own data into a ZIP written to out.
// Channel names appear only for channels the person can still view; nothing
// here names another member.
func (h *Handler) writeArchive(ctx context.Context, user uuid.UUID, out io.Writer) error {
	q := h.Sessions.DB
	viewable, err := h.viewableChannels(ctx, user)
	if err != nil {
		return err
	}
	z := &zipFiles{zw: zip.NewWriter(out)}
	w, err := z.zw.Create("README.txt")
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(exportReadme)); err != nil {
		return err
	}
	steps := []struct {
		name  string
		build func() (any, error)
	}{
		{"profile.json", func() (any, error) { return exportProfile(ctx, q, user) }},
		{"settings.json", func() (any, error) { return exportSettings(ctx, q, user, viewable) }},
		{"edits.json", func() (any, error) { return exportEdits(ctx, q, user) }},
		{"reactions.json", func() (any, error) { return exportReactions(ctx, q, user) }},
		{"poll_votes.json", func() (any, error) { return exportPollVotes(ctx, q, user) }},
		{"uploads.json", func() (any, error) { return exportUploads(ctx, q, user) }},
		{"audit.json", func() (any, error) { return exportAudit(ctx, q, user) }},
		{"security.json", func() (any, error) { return h.exportSecurity(ctx, user) }},
	}
	for _, s := range steps {
		v, err := s.build()
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		if err := z.add(s.name, v); err != nil {
			return err
		}
	}
	mw, err := z.zw.Create("messages.json")
	if err != nil {
		return err
	}
	if err := streamMessages(ctx, q, user, viewable, mw); err != nil {
		return fmt.Errorf("messages.json: %w", err)
	}
	return z.zw.Close()
}

// viewableChannels are the channels the person may still see, by the live
// permission state. Without a permission service no channel name is revealed.
func (h *Handler) viewableChannels(ctx context.Context, user uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if h.Perms == nil {
		return out, nil
	}
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	u, err := GetUser(ctx, h.Sessions.DB, user)
	if err != nil {
		return nil, err
	}
	for _, c := range snap.ViewableChannels(u.Subject()) {
		out[c] = true
	}
	return out, nil
}

func exportProfile(ctx context.Context, q db.Querier, user uuid.UUID) (map[string]any, error) {
	var username, display, bio, role, status, locale, presence string
	var email *string
	var verified, lastSeen, created *time.Time
	var rules, voice, count int64
	var onboarding json.RawMessage
	err := q.QueryRow(ctx, `SELECT username, display_name, bio, role, status_text, locale, presence, email, email_verified_at,
		last_seen_at, created_at, rules_accepted_version, voice_seconds, message_count, onboarding FROM users WHERE id = $1`, user).
		Scan(&username, &display, &bio, &role, &status, &locale, &presence, &email, &verified, &lastSeen, &created, &rules, &voice, &count, &onboarding)
	if err != nil {
		return nil, err
	}
	roles, err := collect(ctx, q, pgx.RowTo[string], `SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = $1 ORDER BY r.position DESC`, user)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": user, "username": username, "display_name": display, "bio": bio, "owner": role == RoleAdmin,
		"status_text": status, "language": locale, "chosen_presence": presence,
		"email": email, "email_confirmed_at": verified, "created_at": created, "last_seen_at": lastSeen,
		"rules_accepted_version": rules, "voice_seconds": voice, "message_count": count,
		"onboarding": onboarding, "roles": roles,
	}, nil
}

// collect runs a query and maps every row.
func collect[T any](ctx context.Context, q db.Querier, fn pgx.RowToFunc[T], sql string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, fn)
}

func (h *Handler) exportSecurity(ctx context.Context, user uuid.UUID) (map[string]any, error) {
	st, err := h.Sessions.twoFactorState(ctx, h.Sessions.DB, user)
	if err != nil {
		return nil, err
	}
	return map[string]any{"two_factor_enabled": st.Enabled, "recovery_codes_remaining": st.RecoveryCodesRemaining}, nil
}
