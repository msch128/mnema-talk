package auth

import (
	"context"
	"errors"
	"io"
	"path"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// exportProfileWithBanner is profile.json plus the banner picture: the stored
// file goes into the archive as banner.<ext> and profile.json names it.
func (h *Handler) exportProfileWithBanner(ctx context.Context, z *zipFiles, user uuid.UUID) (map[string]any, error) {
	profile, err := exportProfile(ctx, h.Sessions.DB, user)
	if err != nil {
		return nil, err
	}
	var key string
	err = h.Sessions.DB.QueryRow(ctx, `
		SELECT m.s3_key FROM users u JOIN media m ON m.id::text = u.banner_s3_key
		WHERE u.id = $1 AND NOT m.is_deleted`, user).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) || h.Objects == nil {
		return profile, nil
	}
	if err != nil {
		return nil, err
	}
	body, getErr := h.Objects.GetObjectFrom(ctx, key, 0)
	if getErr != nil {
		return profile, nil // the file is gone from storage; the rest of the export still counts
	}
	defer body.Close()
	name := "banner" + path.Ext(key)
	w, err := z.zw.Create(name)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(w, body); err != nil {
		return nil, err
	}
	profile["banner_file"] = name
	return profile, nil
}
