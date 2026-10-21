package nativepreview

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

func isValidDecimalString(value string) bool {
	if value == "" {
		return false
	}
	if len(value) > 1 && value[0] == '0' {
		return false
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

func nativeCiphertextDecimal(value string, min, max int64) (int64, bool) {
	if !isValidDecimalString(value) {
		return 0, false
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	if n < min || n > max {
		return 0, false
	}
	return n, true
}

func nativeCiphertextChannel(r *http.Request) (uuid.UUID, error) {
	value := chi.URLParam(r, "channelID")
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, httpx.ErrInvalidInput("invalid native ciphertext channel")
	}
	if id.String() != value {
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

// errNativeCiphertextShape marks a body that is valid JSON of the wrong shape.
var errNativeCiphertextShape = errors.New("native ciphertext body has the wrong shape")

// nativeCiphertextBodyError maps a body failure to the response: too large
// when the size cap was hit, invalid otherwise.
func nativeCiphertextBodyError(err error) error {
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		return httpx.ErrPayloadTooLarge("request body too large")
	}
	return httpx.ErrInvalidInput("invalid native ciphertext body")
}

// checkNativeCiphertextRequest accepts only a single plain application/json
// body that is not known to be too large.
func checkNativeCiphertextRequest(r *http.Request) error {
	types := nativeCiphertextHeaderValues(r, "Content-Type")
	if len(types) != 1 {
		return httpx.ErrUnsupportedMediaType("native ciphertext requires JSON")
	}
	kind, _, err := mime.ParseMediaType(types[0])
	if err != nil || kind != "application/json" {
		return httpx.ErrUnsupportedMediaType("native ciphertext requires JSON")
	}
	if nativePreviewHeaderPresent(r, "Content-Encoding") {
		return httpx.ErrUnsupportedMediaType("native ciphertext requires JSON")
	}
	if r.ContentLength > nativeCiphertextBodyMax {
		return httpx.ErrPayloadTooLarge("request body too large")
	}
	if r.Body == nil {
		return httpx.ErrInvalidInput("invalid native ciphertext body")
	}
	return nil
}

func decodeNativeCiphertext(w http.ResponseWriter, r *http.Request, channel uuid.UUID) (chat.OpaqueCiphertextRequest, error) {
	empty := chat.OpaqueCiphertextRequest{}
	if err := checkNativeCiphertextRequest(r); err != nil {
		return empty, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, nativeCiphertextBodyMax)
	fields, err := readNativeCiphertextFields(json.NewDecoder(r.Body))
	if err != nil {
		return empty, nativeCiphertextBodyError(err)
	}
	event, err := uuid.Parse(fields["client_event_id"])
	if err != nil || event == uuid.Nil {
		return empty, nativeCiphertextBodyError(nil)
	}
	if event.String() != fields["client_event_id"] {
		return empty, nativeCiphertextBodyError(nil)
	}
	group, ok := decodeCanonicalBase64(fields["group_id"], 1, 128)
	if !ok {
		return empty, nativeCiphertextBodyError(nil)
	}
	ciphertext, ok := decodeCanonicalBase64(fields["ciphertext"], 1, 65536)
	if !ok {
		return empty, nativeCiphertextBodyError(nil)
	}
	return chat.OpaqueCiphertextRequest{ChannelID: channel, ClientEventID: event, GroupID: group, Ciphertext: ciphertext}, nil
}

// readNativeCiphertextFields reads exactly one JSON object with the string
// fields client_event_id, group_id and ciphertext, each once and nothing
// else, followed by the end of the body.
func readNativeCiphertextFields(decoder *json.Decoder) (map[string]string, error) {
	if err := readStartObject(decoder); err != nil {
		return nil, err
	}
	fields := make(map[string]string, 3)
	for decoder.More() {
		if err := readFieldEntry(decoder, fields); err != nil {
			return nil, err
		}
	}
	if err := readEndObject(decoder, len(fields)); err != nil {
		return nil, err
	}
	return fields, nil
}

func readStartObject(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errOrShape(err)
	}
	return nil
}

func readFieldEntry(decoder *json.Decoder, fields map[string]string) error {
	key, err := readNativeCiphertextKey(decoder, fields)
	if err != nil {
		return err
	}
	value, err := decoder.Token()
	if err != nil {
		return err
	}
	text, ok := value.(string)
	if !ok {
		return errNativeCiphertextShape
	}
	fields[key] = text
	return nil
}

func readEndObject(decoder *json.Decoder, fieldCount int) error {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('}') {
		return errOrShape(err)
	}
	if fieldCount != 3 {
		return errNativeCiphertextShape
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errOrShape(err)
	}
	return nil
}

// readNativeCiphertextKey reads the next object key, which must be one of the
// three fields and not seen before.
func readNativeCiphertextKey(decoder *json.Decoder, seen map[string]string) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	key, _ := token.(string) // a decoder always yields a string where an object key is due
	if key != "client_event_id" && key != "group_id" && key != "ciphertext" {
		return "", errNativeCiphertextShape
	}
	if _, duplicate := seen[key]; duplicate {
		return "", errNativeCiphertextShape
	}
	return key, nil
}

// errOrShape is err, or the shape error when there is none.
func errOrShape(err error) error {
	if err != nil {
		return err
	}
	return errNativeCiphertextShape
}

// decodeCanonicalBase64 decodes standard padded base64 of min..max bytes that
// re-encodes to exactly the input.
func decodeCanonicalBase64(value string, min, max int) ([]byte, bool) {
	if len(value) > base64.StdEncoding.EncodedLen(max) {
		return nil, false
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) < min {
		return nil, false
	}
	if len(raw) > max || base64.StdEncoding.EncodeToString(raw) != value {
		return nil, false
	}
	return raw, true
}
