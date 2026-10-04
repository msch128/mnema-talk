package chat

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/s3"
)

type MediaRecord struct {
	ID               uuid.UUID  `json:"id"`
	UploaderID       uuid.UUID  `json:"uploader_id"`
	ChannelID        *uuid.UUID `json:"channel_id"`
	MessageID        *uuid.UUID `json:"message_id"`
	S3Bucket         string     `json:"s3_bucket"`
	S3Key            string     `json:"s3_key"`
	OriginalFilename string     `json:"original_filename"`
	MimeType         string     `json:"mime_type"`
	SizeBytes        int64      `json:"size_bytes"`
	IsDeleted        bool       `json:"is_deleted"`
	CreatedAt        time.Time  `json:"created_at"`
}

// UploadHandler handles multipart media uploads to S3 and registers them in PostgreSQL
func UploadHandler(p *db.Pool, s3Cli *s3.Client, bucketName string, maxUploadSizeMB int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := r.Context().Value(auth.UserContextKey).(auth.User)
		if !ok {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		maxBytes := maxUploadSizeMB * 1024 * 1024
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

		if err := r.ParseMultipartForm(maxBytes); err != nil {
			http.Error(w, `{"error":"file exceeds maximum upload limit"}`, http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, `{"error":"file is required in form-data"}`, http.StatusBadRequest)
			return
		}
		defer file.Close()

		channelIDStr := r.FormValue("channel_id")
		var channelID *uuid.UUID
		if chUUID, err := uuid.Parse(channelIDStr); err == nil {
			channelID = &chUUID
		}

		messageIDStr := r.FormValue("message_id")
		var messageID *uuid.UUID
		if msgUUID, err := uuid.Parse(messageIDStr); err == nil {
			messageID = &msgUUID
		}

		// Determine clean filename and extension
		originalFilename := filepath.Base(header.Filename)
		ext := strings.ToLower(filepath.Ext(originalFilename))
		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" || mimeType == "application/octet-stream" {
			detectedMime := mime.TypeByExtension(ext)
			if detectedMime != "" {
				mimeType = detectedMime
			} else {
				mimeType = "application/octet-stream"
			}
		}

		mediaID := uuid.New()
		now := time.Now()
		s3Key := fmt.Sprintf("uploads/%04d/%02d/%s%s", now.Year(), now.Month(), mediaID.String(), ext)

		// 1. Upload file stream to S3
		if err := s3Cli.Upload(r.Context(), s3Key, file, mimeType, header.Size); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"S3 upload failed: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		// 2. Insert record into PostgreSQL
		insertQuery := `
			INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`
		_, err = p.Exec(r.Context(), insertQuery, mediaID, user.ID, channelID, messageID, bucketName, s3Key, originalFilename, mimeType, header.Size)
		if err != nil {
			// Rollback uploaded S3 object on DB insert failure
			_ = s3Cli.Delete(context.Background(), s3Key)
			http.Error(w, fmt.Sprintf(`{"error":"database registration failed: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id":"%s","url":"/api/media/%s","original_filename":%q,"mime_type":%q,"size_bytes":%d}`,
			mediaID.String(), mediaID.String(), originalFilename, mimeType, header.Size)
	}
}

// ServeMediaHandler streams an uploaded media file from S3 to the client with caching headers
func ServeMediaHandler(p *db.Pool, s3Cli *s3.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		mediaID, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid media id", http.StatusBadRequest)
			return
		}

		var s3Key, mimeType string
		var isDeleted bool
		query := `SELECT s3_key, mime_type, is_deleted FROM media WHERE id = $1`
		err = p.QueryRow(r.Context(), query, mediaID).Scan(&s3Key, &mimeType, &isDeleted)
		if err != nil {
			if err == pgx.ErrNoRows {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		if isDeleted {
			http.Error(w, "media has been pruned or deleted", http.StatusGone)
			return
		}

		body, _, size, err := s3Cli.GetObject(r.Context(), s3Key)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer body.Close()

		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

		_, _ = io.Copy(w, body)
	}
}
