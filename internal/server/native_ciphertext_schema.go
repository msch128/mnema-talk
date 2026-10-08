package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const nativeCiphertextBodyMax int64 = 96 << 10

func nativeCiphertextDecimal(value string, min, max int64) (int64, bool) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	for _, b := range []byte(value) {
		if b < '0' || b > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil && n >= min && n <= max
}
func nativeCiphertextChannel(r *http.Request) (uuid.UUID, error) {
	value := chi.URLParam(r, "channelID")
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, httpx.ErrInvalidInput("invalid native ciphertext channel")
	}
	return id, nil
}
func nativeCiphertextHeaderValues(r *http.Request, name string) []string {
	var out []string
	for key, values := range r.Header {
		if strings.EqualFold(key, name) {
			out = append(out, values...)
		}
	}
	return out
}
func decodeNativeCiphertext(w http.ResponseWriter, r *http.Request, channel uuid.UUID) (chat.OpaqueCiphertextRequest, error) {
	empty := chat.OpaqueCiphertextRequest{}
	types := nativeCiphertextHeaderValues(r, "Content-Type")
	if len(types) != 1 {
		return empty, httpx.ErrUnsupportedMediaType("native ciphertext requires JSON")
	}
	kind, _, err := mime.ParseMediaType(types[0])
	if err != nil || kind != "application/json" || nativePreviewHeaderPresent(r, "Content-Encoding") {
		return empty, httpx.ErrUnsupportedMediaType("native ciphertext requires JSON")
	}
	if r.ContentLength > nativeCiphertextBodyMax {
		return empty, httpx.ErrPayloadTooLarge("request body too large")
	}
	if r.Body == nil {
		return empty, httpx.ErrInvalidInput("invalid native ciphertext body")
	}
	r.Body = http.MaxBytesReader(w, r.Body, nativeCiphertextBodyMax)
	invalid := func(err error) (chat.OpaqueCiphertextRequest, error) {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			return empty, httpx.ErrPayloadTooLarge("request body too large")
		}
		return empty, httpx.ErrInvalidInput("invalid native ciphertext body")
	}
	decoder := json.NewDecoder(r.Body)
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return invalid(err)
	}
	fields := make(map[string]string, 3)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return invalid(err)
		}
		key, ok := token.(string)
		if !ok {
			return invalid(nil)
		}
		if key != "client_event_id" && key != "group_id" && key != "ciphertext" {
			return invalid(nil)
		}
		if _, duplicate := fields[key]; duplicate {
			return invalid(nil)
		}
		value, err := decoder.Token()
		if err != nil {
			return invalid(err)
		}
		text, ok := value.(string)
		if !ok {
			return invalid(nil)
		}
		fields[key] = text
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != 3 {
		return invalid(err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalid(err)
	}
	event, err := uuid.Parse(fields["client_event_id"])
	if err != nil || event == uuid.Nil || event.String() != fields["client_event_id"] {
		return invalid(nil)
	}
	decode := func(value string, min, max int) ([]byte, bool) {
		if len(value) > base64.StdEncoding.EncodedLen(max) {
			return nil, false
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(value)
		return raw, err == nil && len(raw) >= min && len(raw) <= max && base64.StdEncoding.EncodeToString(raw) == value
	}
	group, ok := decode(fields["group_id"], 1, 128)
	if !ok {
		return invalid(nil)
	}
	ciphertext, ok := decode(fields["ciphertext"], 1, 65536)
	if !ok {
		return invalid(nil)
	}
	return chat.OpaqueCiphertextRequest{ChannelID: channel, ClientEventID: event, GroupID: group, Ciphertext: ciphertext}, nil
}
