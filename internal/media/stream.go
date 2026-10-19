package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// maxFieldBytes bounds a text field of an upload form (a 4000-character
// caption is at most 16 KB of UTF-8).
const maxFieldBytes = 64 << 10

// uploadFields are an upload form's text fields.
var uploadFields = map[string]bool{"content": true, "parent_id": true, "reply_to_id": true}

// uploadTarget is a validated caption and thread/reply target.
type uploadTarget struct {
	content   string
	parentID  *uuid.UUID
	replyToID *uuid.UUID
}

// storedUpload is a file already written to object storage.
type storedUpload struct {
	id       uuid.UUID
	key      string
	filename string
	mime     string
	size     int64
}

// errFileTooLarge stops a streamed file at the size limit.
var errFileTooLarge = errors.New("file too large")

// readUpload reads a channel upload form as a stream: text fields are kept,
// the file part goes straight to object storage in parts, so neither memory
// nor disk ever holds the whole file. Fields sent before the file (as the web
// app does) are validated before anything is stored; the target is checked
// again once the whole form is read, and the stored file is deleted if the
// form turns out invalid.
func (h *Handler) readUpload(r *http.Request, chID uuid.UUID) (*storedUpload, *uploadTarget, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, nil, httpx.ErrInvalidInput("invalid multipart form")
	}
	ctx := r.Context()
	form := &uploadForm{h: h, ctx: ctx, chID: chID, fields: map[string]string{}}
	fail := func(err error) (*storedUpload, *uploadTarget, error) {
		if form.stored != nil {
			h.discard(ctx, form.stored.key)
		}
		return nil, nil, err
	}

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(h.formError(ctx, err))
		}
		if err := form.addPart(part); err != nil {
			return fail(err)
		}
	}
	if form.stored == nil {
		return nil, nil, httpx.ErrInvalidInput("file file is required")
	}
	target, err := h.validateTarget(ctx, chID, form.fields)
	if err != nil {
		return fail(err)
	}
	return form.stored, target, nil
}

// uploadForm accumulates the parts of an upload form as they stream in.
type uploadForm struct {
	h      *Handler
	ctx    context.Context
	chID   uuid.UUID
	fields map[string]string
	stored *storedUpload
}

// addPart handles one form part: the file is validated against the fields
// seen so far and stored, known text fields are kept, unknown parts are
// skipped (NextPart discards what is left of them).
func (f *uploadForm) addPart(part *multipart.Part) error {
	name := part.FormName()
	switch {
	case name == "file":
		return f.storeFile(part)
	case uploadFields[name]:
		b, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
		if err != nil {
			return f.h.formError(f.ctx, err)
		}
		if len(b) > maxFieldBytes {
			return httpx.ErrInvalidInput(name + " is too long")
		}
		f.fields[name] = string(b)
	}
	return nil
}

func (f *uploadForm) storeFile(part *multipart.Part) (err error) {
	if f.stored != nil {
		return httpx.ErrInvalidInput("only one file per upload")
	}
	if _, err := f.h.validateTarget(f.ctx, f.chID, f.fields); err != nil {
		return err
	}
	f.stored, err = f.h.storePart(f.ctx, part)
	return err
}

// validateTarget checks the caption and the thread/reply target.
func (h *Handler) validateTarget(ctx context.Context, chID uuid.UUID, fields map[string]string) (*uploadTarget, error) {
	content, err := chat.ValidateContent(fields["content"], true)
	if err != nil {
		return nil, err
	}
	t := &uploadTarget{content: content}
	for name, dst := range map[string]**uuid.UUID{"parent_id": &t.parentID, "reply_to_id": &t.replyToID} {
		raw := fields[name]
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, httpx.ErrInvalidInput("invalid " + name)
		}
		*dst = &id
	}
	if err := chat.ValidateTarget(ctx, h.DB, chID, t.parentID, t.replyToID); err != nil {
		return nil, err
	}
	return t, nil
}

// storePart sniffs the file's type from its first bytes, then streams it to
// object storage, stopping at the request's upload limit (see WithUploadLimit).
func (h *Handler) storePart(ctx context.Context, part *multipart.Part) (*storedUpload, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(part, head)
	if n == 0 && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
		return nil, httpx.ErrInvalidInput("file is empty")
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, h.formError(ctx, err)
	}

	filename := httpx.SanitizeFilename(part.FileName(), maxFilenameLen)
	if filename == "" {
		filename = "file"
	}
	mimeType, err := reconcileMIME(declaredMIME(part.Header.Get("Content-Type"), filename), head[:n], allowedMIME, false)
	if err != nil {
		return nil, httpx.ErrUnsupportedMediaType(err.Error())
	}

	body := &cappedReader{r: io.MultiReader(bytes.NewReader(head[:n]), part), max: h.limit(ctx)}
	id := uuid.New()
	key := storageKey("uploads", id, mimeType)
	if err := h.Store.Upload(ctx, key, body, mimeType, -1); err != nil {
		// A failed streamed upload stores nothing (multipart is aborted).
		if errors.Is(err, errFileTooLarge) {
			return nil, httpx.ErrPayloadTooLarge(fmt.Sprintf("file exceeds the %d MB limit", h.limit(ctx)>>20))
		}
		if body.readErr != nil {
			// The client's body failed (limit, disconnect, broken framing),
			// not the storage.
			return nil, h.formError(ctx, body.readErr)
		}
		return nil, fmt.Errorf("store upload: %w", err)
	}
	return &storedUpload{id: id, key: key, filename: filename, mime: mimeType, size: body.n}, nil
}

// formError maps a failure reading the request body: the transport limit is
// a 413, anything else a malformed form.
func (h *Handler) formError(ctx context.Context, err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return httpx.ErrPayloadTooLarge(fmt.Sprintf("file exceeds the %d MB limit", h.limit(ctx)>>20))
	}
	return httpx.ErrInvalidInput("invalid multipart form")
}

// discard deletes a stored upload that will not be used.
func (h *Handler) discard(ctx context.Context, key string) {
	if err := h.Store.Delete(context.WithoutCancel(ctx), key); err != nil {
		slog.Warn("orphaned upload", "key", key, "err", err)
	}
}

// cappedReader counts what it reads and fails with errFileTooLarge once
// more than max bytes came through. readErr keeps a failure of the
// underlying body, to tell it apart from a storage error.
type cappedReader struct {
	r       io.Reader
	max     int64
	n       int64
	readErr error
}

func (c *cappedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n > c.max {
		return n, errFileTooLarge
	}
	if err != nil && !errors.Is(err, io.EOF) {
		c.readErr = err
	}
	return n, err
}
