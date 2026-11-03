package auth

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

// exportEmoji lists the custom emoji the member uploaded and adds their image
// files to the archive under emoji/. An image that is gone from storage is
// listed without a file; any other failure fails the export.

type exportedEmoji struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	ContentType string    `json:"content_type" db:"content_type"`
	Animated    bool      `json:"animated" db:"animated"`
	SizeBytes   int       `json:"size_bytes" db:"size_bytes"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	File        string    `json:"file,omitempty" db:"-"`
}

type emojiRow struct {
	exportedEmoji
	Key string `db:"s3_key"`
}

func (h *Handler) exportEmoji(ctx context.Context, z *zipFiles, user uuid.UUID) (capped, error) {
	rows, cut, err := fetchRows[emojiRow](ctx, h.Sessions.DB, exportMaxRows, `
		SELECT id, name, content_type, animated, size_bytes, created_at, s3_key
		FROM custom_emoji WHERE created_by = $1 ORDER BY created_at LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	out := make([]exportedEmoji, len(rows))
	for i, r := range rows {
		e := r.exportedEmoji
		body, found, err := h.openExportObject(ctx, r.Key)
		if err != nil {
			return capped{}, err
		}
		if found {
			if e.File, err = copyEmojiImage(z.zw, e, body); err != nil {
				return capped{}, err
			}
		}
		out[i] = e
	}
	return listOf(out, cut), nil
}

// copyEmojiImage streams one emoji file into the archive and closes it.
func copyEmojiImage(zw *zip.Writer, e exportedEmoji, body io.ReadCloser) (string, error) {
	defer body.Close()
	ext := "png"
	if e.ContentType == "image/gif" {
		ext = "gif"
	}
	name := fmt.Sprintf("emoji/%s.%s", e.Name, ext)
	w, err := zw.Create(name)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(w, body)
	return name, err
}
