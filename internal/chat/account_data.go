package chat

import (
	"context"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// accountData is the chat part of a personal-data export and of account erasure.
type accountData struct{}

// RegisterAccountData adds chat's export files and erasure statements.
func RegisterAccountData(r *userdata.Registry) {
	var d accountData
	r.Register(d, d)
}

func (accountData) Export(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, limits userdata.Limits) ([]userdata.File, error) {
	return []userdata.File{
		editFile(ctx, q, user, limits),
		reactionFile(ctx, q, user, limits),
		pollVoteFile(ctx, q, user, limits),
		uploadFile(ctx, q, user, limits),
		pollFile(ctx, q, user, channels, limits),
		pinFile(ctx, q, user, channels, limits),
		threadFollowFile(ctx, q, user, limits),
		directMessageFile(ctx, q, user, limits),
		blockFile(ctx, q, user, limits),
		reportFile(ctx, q, user, limits),
		savedFile(ctx, q, user, limits),
		messageFile(ctx, q, user, channels, limits),
	}, nil
}

func (accountData) Erase() []userdata.Step { return chatEraseSteps() }

func listFile[T any](name string, order int, load func() ([]T, bool, error)) userdata.File {
	return userdata.File{
		Name: name, Order: order,
		Build: func() (any, bool, error) {
			rows, cut, err := load()
			if err != nil {
				return nil, false, err
			}
			return userdata.AsList(rows), cut, nil
		},
	}
}

func userRows[T any](ctx context.Context, q db.Querier, user uuid.UUID, limit int, sql string) ([]T, bool, error) {
	return userdata.FetchRows[T](ctx, q, limit, sql, user)
}
