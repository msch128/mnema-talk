package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
)

// rawPayload is the lenient decoding of ExecutePayload.
type rawPayload struct {
	Content         string           `json:"content"`
	Username        string           `json:"username"`
	Embeds          []inEmbed        `json:"embeds"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions"`
}

// request is the parsed body of an execute call.
type request struct {
	payload rawPayload
	files   []*media.StoredFile
}

// readRequest parses the JSON or multipart body. Files are already stored;
// the caller records or discards them.
func (h *Handler) readRequest(w http.ResponseWriter, r *http.Request) (*request, error) {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return nil, formError("Content-Type must be application/json or multipart/form-data")
	}
	req := &request{}
	switch mt {
	case "application/json":
		httpx.SetJSONReadDeadline(w)
		r.Body = http.MaxBytesReader(w, r.Body, httpx.DefaultMaxBody)
		if err := decodeLenient(r.Body, &req.payload); err != nil {
			return nil, h.bodyError(err)
		}
		return req, nil
	case "multipart/form-data":
		ctx, body, err := h.Media.GlobalLimit(r.Context())
		if err != nil {
			return nil, err
		}
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, body)
		return req, h.readMultipart(r, req)
	}
	return nil, formError("Content-Type must be application/json or multipart/form-data")
}

func decodeLenient(body io.Reader, dst any) error {
	dec := json.NewDecoder(body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

func (h *Handler) bodyError(err error) error {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return httpx.ErrPayloadTooLarge("request body too large")
	}
	return formError("the body is not a valid JSON object")
}

func (h *Handler) readMultipart(r *http.Request, req *request) error {
	mr, err := r.MultipartReader()
	if err != nil {
		return formError("invalid multipart form")
	}
	ctx := r.Context()
	form := map[string]string{}
	var jsonPart bool
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return h.discardFiles(ctx, req, h.Media.FormError(ctx, err))
		}
		isJSON, err := h.readPart(ctx, part, req, form)
		if err != nil {
			return h.discardFiles(ctx, req, err)
		}
		jsonPart = jsonPart || isJSON
	}
	// Plain form fields stand in for a missing payload_json, as at Discord.
	if !jsonPart {
		req.payload.Content = form["content"]
		req.payload.Username = form["username"]
	}
	return nil
}

// discardFiles removes the files stored so far and passes err through.
func (h *Handler) discardFiles(ctx context.Context, req *request, err error) error {
	for _, f := range req.files {
		h.Media.DiscardFile(ctx, f)
	}
	req.files = nil
	return err
}

// readPart handles one multipart part: a file, the payload_json document, or
// a plain content/username field (collected in form). It reports whether the
// part was payload_json.
func (h *Handler) readPart(ctx context.Context, part *multipart.Part, req *request, form map[string]string) (bool, error) {
	switch {
	case part.FileName() != "":
		return false, h.storePartFile(ctx, part, req)
	case part.FormName() == "payload_json":
		return true, h.readPayloadJSON(ctx, part, req)
	case part.FormName() == "content" || part.FormName() == "username":
		b, err := io.ReadAll(io.LimitReader(part, chat.MaxMessageLen*4+1))
		if err != nil {
			return false, h.Media.FormError(ctx, err)
		}
		form[part.FormName()] = string(b)
	}
	return false, nil
}

func (h *Handler) storePartFile(ctx context.Context, part *multipart.Part, req *request) error {
	if len(req.files) >= maxFiles {
		return formError("at most 10 files are allowed")
	}
	f, err := h.Media.StoreFilePart(ctx, part)
	if err != nil {
		return err
	}
	req.files = append(req.files, f)
	return nil
}

func (h *Handler) readPayloadJSON(ctx context.Context, part *multipart.Part, req *request) error {
	b, err := io.ReadAll(io.LimitReader(part, maxPayloadJSON+1))
	if err != nil {
		return h.Media.FormError(ctx, err)
	}
	if len(b) > maxPayloadJSON {
		return httpx.ErrPayloadTooLarge("payload_json too large")
	}
	if err := decodeLenient(strings.NewReader(string(b)), &req.payload); err != nil {
		return formError("payload_json is not a valid JSON object")
	}
	return nil
}

// emptyMessage and unknownThread are executed-call errors with their own
// Discord codes.
type emptyMessage struct{}

func (emptyMessage) Error() string { return "Cannot send an empty message" }

type unknownThread struct{}

func (unknownThread) Error() string { return "Unknown Channel" }
