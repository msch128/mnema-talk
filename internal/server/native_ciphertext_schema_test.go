package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func relayUnitBody(event uuid.UUID, group, ciphertext []byte) []byte {
	raw, _ := json.Marshal(map[string]string{"client_event_id": event.String(), "group_id": base64.StdEncoding.EncodeToString(group), "ciphertext": base64.StdEncoding.EncodeToString(ciphertext)})
	return raw
}
func relayUnitDecode(raw []byte, change func(*http.Request)) (chat.OpaqueCiphertextRequest, int) {
	r := httptest.NewRequest(http.MethodPost, "https://community.example.invalid", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if change != nil {
		change(r)
	}
	w := httptest.NewRecorder()
	input, err := decodeNativeCiphertext(w, r, uuid.New())
	if err != nil {
		httpx.WriteError(w, err)
		return input, w.Code
	}
	return input, 200
}
func TestNativeCiphertextSchemaExactBoundsAndAmbiguity(t *testing.T) {
	event := uuid.MustParse("abcdefab-1234-4567-8901-123456789abc")
	valid := relayUnitBody(event, []byte{1}, []byte{0, 255, 7})
	input, status := relayUnitDecode(valid, nil)
	if status != 200 || input.ClientEventID != event || !bytes.Equal(input.Ciphertext, []byte{0, 255, 7}) {
		t.Fatal("valid opaque wire changed")
	}
	for _, raw := range [][]byte{
		nil, []byte(`[]`), []byte(`null`), []byte(`{}`), []byte(`{`), []byte(`{"client_event_id":`), []byte(`{"client_event_id":null}`), []byte(`{"client_event_id":[]}`), []byte(`{"unknown":"x"}`),
		[]byte(`{"client_event_id":"` + event.String() + `","client_event_id":"` + event.String() + `","group_id":"AQ==","ciphertext":"AQ=="}`),
		append(append([]byte{}, valid...), []byte(` {}`)...),
		relayUnitBody(uuid.Nil, []byte{1}, []byte{1}), relayUnitBody(event, nil, []byte{1}), relayUnitBody(event, make([]byte, 129), []byte{1}), relayUnitBody(event, make([]byte, 130), []byte{1}), relayUnitBody(event, []byte{1}, nil), relayUnitBody(event, []byte{1}, make([]byte, 65537)),
		[]byte(`{"client_event_id":"` + strings.ToUpper(event.String()) + `","group_id":"AQ==","ciphertext":"AQ=="}`),
		[]byte(`{"client_event_id":"` + event.String() + `","group_id":"AR==","ciphertext":"AQ=="}`),
		[]byte(`{"client_event_id":"` + event.String() + `","group_id":"AQ==\n","ciphertext":"AQ=="}`),
		[]byte(`{"client_event_id":"` + event.String() + `","group_id":"AQ==","ciphertext":"_w=="}`),
	} {
		if _, status := relayUnitDecode(raw, nil); status != 400 {
			t.Fatal("ambiguous or invalid opaque schema accepted")
		}
	}
	if _, status := relayUnitDecode(relayUnitBody(event, make([]byte, 128), make([]byte, 65536)), nil); status != 200 {
		t.Fatal("valid maximum opaque body denied")
	}
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Content-Type") }, func(r *http.Request) { r.Header["Content-Type"] = []string{"application/json", "application/json"} }, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, func(r *http.Request) { r.Header.Set("Content-Type", "invalid;") }, func(r *http.Request) { r.Header["Content-Encoding"] = []string{""} },
	} {
		if _, status := relayUnitDecode(valid, change); status != 415 {
			t.Fatal("invalid content framing admitted")
		}
	}
	if _, status := relayUnitDecode(valid, func(r *http.Request) { r.Body = nil }); status != 400 {
		t.Fatal("nil body admitted")
	}
	if _, status := relayUnitDecode(valid, func(r *http.Request) { r.ContentLength = nativeCiphertextBodyMax + 1 }); status != 413 {
		t.Fatal("oversized known body admitted")
	}
	huge := append([]byte(`{"ciphertext":"`), bytes.Repeat([]byte("A"), int(nativeCiphertextBodyMax))...)
	if _, status := relayUnitDecode(huge, func(r *http.Request) { r.ContentLength = -1 }); status != 413 {
		t.Fatal("oversized streaming body admitted")
	}
}
func TestNativeCiphertextCanonicalPaginationAndChannel(t *testing.T) {
	for _, query := range []string{"", "after=0", "limit=1", "after=9223372036854775807&limit=100"} {
		u, _ := url.Parse("https://community.example.invalid/events?" + query)
		u.ForceQuery = false
		if _, err := parseNativeCiphertextQuery(http.MethodGet, u); err != nil {
			t.Fatal("valid fixed pagination denied")
		}
	}
	for _, query := range []string{"after=-1", "after=01", "after=+1", "after=9223372036854775808", "limit=0", "limit=101", "limit=1&after=0", "after=0&after=1", "unknown=1", "a=1&b=2&c=3", "after=%31", "%61fter=1", "after=1&", "after=1;limit=1", "after=%zz", "limit="} {
		u := &url.URL{RawQuery: query}
		if _, err := parseNativeCiphertextQuery(http.MethodGet, u); err == nil {
			t.Fatal("invalid pagination admitted")
		}
	}
	if _, err := parseNativeCiphertextQuery(http.MethodGet, &url.URL{ForceQuery: true}); err == nil {
		t.Fatal("empty query delimiter admitted")
	}
	if _, err := parseNativeCiphertextQuery(http.MethodPost, &url.URL{RawQuery: "after=0"}); err == nil {
		t.Fatal("publish query admitted")
	}
	if _, err := parseNativeCiphertextQuery(http.MethodPost, &url.URL{}); err != nil {
		t.Fatal("queryless publish denied")
	}
	for _, value := range []string{"", "-1", "01", "x", "9223372036854775808"} {
		if _, ok := nativeCiphertextDecimal(value, 0, 100); ok {
			t.Fatal("invalid integer admitted")
		}
	}
	id := uuid.MustParse("abcdefab-1234-4567-8901-123456789abc")
	for _, value := range []string{id.String(), uuid.Nil.String(), strings.ToUpper(id.String()), "invalid"} {
		r := httptest.NewRequest(http.MethodGet, "https://community.example.invalid", nil)
		ctx := chi.NewRouteContext()
		ctx.URLParams.Add("channelID", value)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
		_, err := nativeCiphertextChannel(r)
		if (err == nil) != (value == id.String()) {
			t.Fatal("channel canonical identity failed")
		}
	}
	if !nativeCiphertextRoute(NativePreviewPrefix+"/channels/"+id.String()+"/ciphertext-events") || nativeCiphertextRoute(NativePreviewPrefix+"/channels/a/b/ciphertext-events") {
		t.Fatal("fixed relay routing failed")
	}
}
func TestNativeCiphertextBoundaryAndSafeErrors(t *testing.T) {
	preview := &NativeCiphertextPreviewRouter{NativePreviewRouter: &NativePreviewRouter{options: NativePreviewOptions{Compatibility: NativeSupported}}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := r.Context().Value(nativeCiphertextPagingKey{}).(nativeCiphertextPaging)
		if !ok || p.After != 2 || p.Limit != 3 || r.URL.RawQuery != "" || r.URL.ForceQuery {
			t.Error("typed pagination transfer failed")
		}
		w.WriteHeader(204)
	})
	request := func(change func(*http.Request), want int) {
		r := httptest.NewRequest(http.MethodGet, "https://community.example.invalid/events?after=2&limit=3", nil)
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		preview.relayBoundary(next).ServeHTTP(w, r)
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("native relay boundary failed")
		}
	}
	request(nil, 204)
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header["origin"] = []string{""} }, func(r *http.Request) { r.Header["Cookie"] = []string{""} }, func(r *http.Request) { r.URL.User = url.User("synthetic") }, func(r *http.Request) { r.URL.Fragment = "x" }, func(r *http.Request) { r.URL.RawPath = "/escaped" }} {
		request(change, 403)
	}
	request(func(r *http.Request) { r.URL.RawQuery = "unknown=x" }, 400)
	preview.options.Compatibility = NativeUnsupported
	request(nil, 503)
	for _, tc := range []struct {
		err    error
		status int
	}{{auth.ErrNativeUnauthorized, 401}, {chat.ErrNativeContentInput, 400}, {chat.ErrNativeContentChannel, 404}, {chat.ErrNativeContentConflict, 409}, {errors.New("private fixture diagnostic must not appear"), 503}} {
		w := httptest.NewRecorder()
		httpx.WriteError(w, nativeCiphertextError(tc.err))
		if w.Code != tc.status || strings.Contains(w.Body.String(), "private fixture diagnostic") {
			t.Fatal("unsafe error mapping")
		}
	}
	if _, err := NewNativeCiphertextPreviewRouter(Deps{}, NativePreviewOptions{}); err == nil {
		t.Fatal("nil dependencies accepted")
	}
	if _, err := NewNativeCiphertextPreviewRouter(Deps{Config: &config.Config{TrustedProxies: []string{"invalid"}}}, NativePreviewOptions{}); err == nil {
		t.Fatal("invalid trust accepted")
	}
	if _, err := NewNativeCiphertextPreviewRouter(Deps{Config: &config.Config{}}, NativePreviewOptions{}); err == nil {
		t.Fatal("invalid base router accepted")
	}
	for _, handler := range []func(http.ResponseWriter, *http.Request) error{preview.publishCiphertext, preview.listCiphertext} {
		if handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://community.example.invalid", nil)) == nil {
			t.Fatal("missing principal accepted")
		}
	}
}
