package push

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// accountData is the member's push devices in an export, and their removal
// when the account is erased. Endpoints and keys are not exported.
type accountData struct{}

// RegisterAccountData adds the push export and the subscription erasure.
func RegisterAccountData(r *userdata.Registry) {
	var d accountData
	r.Register(d, d)
}

func (accountData) Export(ctx context.Context, q db.Querier, user uuid.UUID, _ userdata.Channels, limits userdata.Limits) ([]userdata.File, error) {
	return []userdata.File{pushFile(ctx, q, user, limits)}, nil
}

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{{Order: userdata.OrderPush, SQL: `DELETE FROM push_subscriptions WHERE user_id = $1`}}
}

func pushFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	return userdata.File{
		Name: "push.json", Order: userdata.FilePush,
		Build: func() (any, bool, error) { return exportPush(ctx, q, user, limits.MaxRows) },
	}
}

// exportPush describes the member's push setup: the choice about message text
// and the devices. Never the endpoint or the keys.
func exportPush(ctx context.Context, q db.Querier, user uuid.UUID, maxRows int) (any, bool, error) {
	type device struct {
		Label         string     `json:"label" db:"label"`
		CreatedAt     time.Time  `json:"created_at" db:"created_at"`
		LastSuccessAt *time.Time `json:"last_success_at" db:"last_success_at"`
	}
	devices, cut, err := userdata.FetchRows[device](ctx, q, maxRows, `
		SELECT label, created_at, last_success_at FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at LIMIT $2`, user)
	if err != nil {
		return nil, false, err
	}
	var showText bool
	if err := q.QueryRow(ctx, `SELECT push_preview FROM users WHERE id = $1`, user).Scan(&showText); err != nil {
		return nil, false, err
	}
	return map[string]any{"show_message_text": showText, "devices": devices}, cut, nil
}
