package server

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

// notifiers tells several parties about permission changes.
type notifiers []perm.Notifier

func (n notifiers) PermissionsChanged(userIDs []uuid.UUID) {
	for _, x := range n {
		x.PermissionsChanged(userIDs)
	}
}

// auditActor names the signed-in user of a context for the audit log.
func auditActor(ctx context.Context) (uuid.UUID, string, bool) {
	if u := auth.UserFrom(ctx); u != nil {
		return u.ID, u.Username, true
	}
	return uuid.Nil, "", false
}

// auditViewer says what the signed-in user may read in the audit log: the
// Owner and Administrators everything, others the entries of the channels
// they can view (audit.Classes).
func auditViewer(svc *perm.Service) func(context.Context) (audit.Viewer, error) {
	return func(ctx context.Context) (audit.Viewer, error) {
		u := auth.UserFrom(ctx)
		if u == nil {
			return audit.Viewer{}, nil
		}
		snap, err := svc.Snapshot(ctx)
		if err != nil {
			return audit.Viewer{}, err
		}
		sub := u.Subject()
		if snap.Global(sub).Has(perm.Administrator) {
			return audit.Viewer{Privileged: true}, nil
		}
		return audit.Viewer{Channels: snap.ViewableChannels(sub)}, nil
	}
}

// refreshPreviewDeny keeps the server's changing public address excluded from
// link previews until the router closes.
func refreshPreviewDeny(ctx context.Context, interval time.Duration, deny func()) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			deny()
		}
	}
}
