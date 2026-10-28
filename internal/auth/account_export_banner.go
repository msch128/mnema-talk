package auth

import (
	"context"
	"errors"
	"io"
	"path"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// exportProfileWithImages is profile.json plus the profile pictures: the
// stored files go into the archive as avatar.<ext> and banner.<ext>, and
// profile.json names them.
func (h *Handler) exportProfileWithImages(ctx context.Context, z *zipFiles, user uuid.UUID) (map[string]any, error) {
	profile, err := exportProfile(ctx, h.Sessions.DB, user)
	if err != nil {
		return nil, err
	}
	for _, img := range []struct{ column, name string }{
		{"avatar_s3_key", "avatar"},
		{"banner_s3_key", "banner"},
	} {
		file, err := h.exportProfileImage(ctx, z, user, img.column, img.name)
		if err != nil {
			return nil, err
		}
		if file != "" {
			profile[img.name+"_file"] = file
		}
	}
	return profile, nil
}

// exportProfileImage copies one profile picture (column holds the id of its
// media row) into the archive and returns the file name, or "" when there is
// no picture or it is gone from storage.
func (h *Handler) exportProfileImage(ctx context.Context, z *zipFiles, user uuid.UUID, column, base string) (string, error) {
	if h.Objects == nil {
		return "", nil
	}
	var ref *string
	if err := h.Sessions.DB.QueryRow(ctx, `SELECT `+column+` FROM users WHERE id = $1`, user).Scan(&ref); err != nil {
		return "", err
	}
	if ref == nil {
		return "", nil
	}
	mediaID, err := uuid.Parse(*ref)
	if err != nil {
		return "", nil
	}
	var key string
	err = h.Sessions.DB.QueryRow(ctx, `SELECT s3_key FROM media WHERE id = $1 AND NOT is_deleted`, mediaID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	body, found, err := h.openExportObject(ctx, key)
	if err != nil || !found {
		return "", err
	}
	defer body.Close()
	name := base + path.Ext(key)
	w, err := z.zw.Create(name)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(w, body); err != nil {
		return "", err
	}
	return name, nil
}

// openExportObject opens a stored file for the archive. found is false when
// the file is gone from storage, which the export lists without it; any other
// failure (a timeout, a server error) is returned, so the export fails and can
// be requested again instead of being handed out incomplete.
func (h *Handler) openExportObject(ctx context.Context, key string) (body io.ReadCloser, found bool, err error) {
	body, err = h.Objects.GetObjectFrom(ctx, key, 0)
	if err != nil {
		if h.ObjectMissing != nil && h.ObjectMissing(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return body, true, nil
}
