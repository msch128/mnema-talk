package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func nativeUnitHandler(t *testing.T) *NativeHandler {
	t.Helper()
	accounts := newLoginHandler()
	accounts.Sessions.DB = &db.Pool{}
	handler, err := NewNativeHandler(accounts, &NativeSessions{pool: accounts.Sessions.DB})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func nativeRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.20:1234"
	r.Header.Set("Content-Type", "application/json")
	return r
}

func nativeStatus(t *testing.T, handler http.Handler, request *http.Request, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, request)
	if w.Code != want || w.Header().Get("Cache-Control") != "no-store" || len(w.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("native response status/cache/cookie mismatch: status %d expected %d", w.Code, want)
	}
	return w
}

func TestNativeHandlerDependenciesMustShareAccountInstance(t *testing.T) {
	h := nativeUnitHandler(t)
	for _, pair := range []struct {
		accounts *Handler
		sessions *NativeSessions
	}{{nil, h.sessions}, {h.accounts, nil}, {&Handler{}, h.sessions},
		{&Handler{Sessions: &Sessions{}}, h.sessions}, {newLoginHandler(), h.sessions},
		{h.accounts, &NativeSessions{pool: &db.Pool{}}}} {
		if _, err := NewNativeHandler(pair.accounts, pair.sessions); err == nil {
			t.Fatal("invalid native dependencies accepted")
		}
	}
	for _, remove := range []func(*Handler){func(a *Handler) { a.loginPerIP = nil },
		func(a *Handler) { a.loginFailures = nil }, func(a *Handler) { a.accountFailures = nil }} {
		copy := *h.accounts
		remove(&copy)
		if _, err := NewNativeHandler(&copy, h.sessions); err == nil {
			t.Fatal("missing shared rate limiter accepted")
		}
	}
}

func TestNativeRequestBoundaryRejectsBrowserHeadersAndTargets(t *testing.T) {
	h := nativeUnitHandler(t)
	for _, name := range []string{"Origin", "origin", "Cookie", "cOoKiE"} {
		for _, values := range [][]string{nil, {}, {""}, {"https://example.invalid"}} {
			r := nativeRequest(http.MethodPost, "/auth/login", "{}")
			r.Header[name] = values
			nativeStatus(t, h.Handler(), r, http.StatusForbidden)
			nativeStatus(t, h.RequireUser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("browser request admitted") })), r, http.StatusForbidden)
		}
	}
	for _, alter := range []func(*http.Request){func(r *http.Request) { r.URL.RawQuery = "token=private" },
		func(r *http.Request) { r.URL.Fragment = "private" }, func(r *http.Request) { r.URL.User = url.UserPassword("user", "private") }} {
		r := nativeRequest(http.MethodGet, "/auth/me", "")
		alter(r)
		nativeStatus(t, h.Handler(), r, http.StatusBadRequest)
		nativeStatus(t, h.RequireUser(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("target admitted") })), r, http.StatusForbidden)
	}
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodGet, "/missing", ""), http.StatusNotFound)
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodGet, "/auth/login", ""), http.StatusMethodNotAllowed)
}

func TestNativeBearerRequiresOneExactCanonicalHeader(t *testing.T) {
	good := "Bearer " + strings.Repeat("A", 43)
	for _, value := range []string{"", "Bearer", strings.ToLower(good), " " + good, good + " ", good + "," + good,
		strings.Replace(good, " ", "\t", 1), "Bearer " + strings.Repeat("A", 42) + "B"} {
		r := nativeRequest(http.MethodGet, "/auth/me", "")
		r.Header.Set("Authorization", value)
		if token, err := nativeBearer(r); err != ErrNativeUnauthorized || token != "" {
			t.Fatal("noncanonical authorization admitted")
		}
	}
	for _, headers := range []http.Header{{}, {"Authorization": nil}, {"Authorization": {good, good}},
		{"Authorization": {good}, "authorization": {good}}} {
		r := nativeRequest(http.MethodGet, "/auth/me", "")
		r.Header = headers
		if token, err := nativeBearer(r); err != ErrNativeUnauthorized || token != "" {
			t.Fatal("missing/duplicate authorization admitted")
		}
	}
	r := nativeRequest(http.MethodGet, "/auth/me", "")
	r.Header["authorization"] = []string{good}
	if token, err := nativeBearer(r); err != nil || token != good[7:] {
		t.Fatal("canonical bearer rejected")
	}
	for _, route := range []string{"/auth/login", "/auth/refresh"} {
		r := nativeRequest(http.MethodPost, route, "{}")
		r.Header["Authorization"] = nil
		nativeStatus(t, nativeUnitHandler(t).Handler(), r, http.StatusBadRequest)
	}
	nativeStatus(t, nativeUnitHandler(t).Handler(), nativeRequest(http.MethodGet, "/auth/me", ""), http.StatusUnauthorized)
}

func TestNativeStrictJSONRejectsAmbiguousBodiesAndContentTypes(t *testing.T) {
	for _, body := range []string{"", "null", "[]", `{"a":"x","a":"y"}`, `{"b":"x"}`, `{}`, `{"a":null}`,
		`{"a":1}`, `{"a":{}}`, `{"a":"x"} {}`, `{"a":"x"`, `{"a":`, `{"a":"x",}`, `{"a":"x"]`, `{"a":"x"} private`} {
		r := nativeRequest(http.MethodPost, "/", body)
		if _, err := decodeNativeStrings(httptest.NewRecorder(), r, "a"); err == nil {
			t.Fatal("ambiguous native JSON accepted")
		}
	}
	for _, headers := range []http.Header{{}, {"Content-Type": {"text/plain"}},
		{"Content-Type": {"application/json; broken"}}, {"Content-Type": {"application/json", "application/json"}},
		{"Content-Type": {"application/json"}, "content-type": {"application/json"}}} {
		r := nativeRequest(http.MethodPost, "/", `{"a":"x"}`)
		r.Header = headers
		_, err := decodeNativeStrings(httptest.NewRecorder(), r, "a")
		api, ok := httpx.AsAPIError(err)
		if !ok || api.Status != http.StatusUnsupportedMediaType {
			t.Fatal("invalid native content type accepted")
		}
	}
	r := nativeRequest(http.MethodPost, "/", ` {"a":"x"} `)
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	fields, err := decodeNativeStrings(httptest.NewRecorder(), r, "a")
	if err != nil || fields["a"] != "x" {
		t.Fatal("canonical native JSON rejected")
	}
	for _, knownSize := range []bool{true, false} {
		r := nativeRequest(http.MethodPost, "/", `{"a":"`+strings.Repeat("x", int(nativeAuthMaxBody))+`"}`)
		if !knownSize {
			r.ContentLength = -1
		}
		_, err := decodeNativeStrings(httptest.NewRecorder(), r, "a")
		api, ok := httpx.AsAPIError(err)
		if !ok || api.Status != http.StatusRequestEntityTooLarge {
			t.Fatal("native body limit bypassed")
		}
	}
	r = nativeRequest(http.MethodPost, "/", "")
	r.Body = nil
	if _, err := decodeNativeStrings(httptest.NewRecorder(), r, "a"); err == nil {
		t.Fatal("nil native body accepted")
	}
}

func TestNativeEmptyBodyAndMissingPrincipalFailClosed(t *testing.T) {
	for _, body := range []io.ReadCloser{nil, http.NoBody, io.NopCloser(strings.NewReader(""))} {
		r := nativeRequest(http.MethodGet, "/auth/me", "")
		r.Body = body
		if err := emptyNativeBody(r); err != nil {
			t.Fatal("empty body rejected")
		}
	}
	h := nativeUnitHandler(t)
	for _, body := range []string{" ", "{}", "private"} {
		r := nativeRequest(http.MethodGet, "/auth/me", body)
		if err := emptyNativeBody(r); err == nil {
			t.Fatal("nonempty body accepted")
		}
		if err := h.me(httptest.NewRecorder(), r); err == nil {
			t.Fatal("me body accepted")
		}
		if err := h.logout(httptest.NewRecorder(), r); err == nil {
			t.Fatal("logout body accepted")
		}
	}
	for _, fn := range []func(http.ResponseWriter, *http.Request) error{h.me, h.logout} {
		err := fn(httptest.NewRecorder(), nativeRequest(http.MethodGet, "/", ""))
		api, ok := httpx.AsAPIError(err)
		if !ok || api.Status != http.StatusUnauthorized {
			t.Fatal("missing native context accepted")
		}
	}
	if _, ok := NativePrincipalFrom(context.Background()); ok {
		t.Fatal("missing native principal appeared")
	}
}

func TestNativeWireConversionExplicitAndFormattingRedacted(t *testing.T) {
	secret := newNativeSecret()
	for i := range *secret.value {
		(*secret.value)[i] = byte(i + 1)
	}
	wire := nativeWireSecret{value: secret}
	grant := nativeGrantResponse{AccessToken: wire, RefreshToken: wire}
	for _, value := range []any{wire, &wire, grant, &grant, struct{ secret nativeWireSecret }{wire}, struct{ grant nativeGrantResponse }{grant}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%b", "%o", "%O", "%x", "%X", "%e", "%E", "%f", "%F", "%g", "%G", "%c", "%U", "%020d"} {
			text := fmt.Sprintf(format, value)
			if strings.Contains(text, secret.wire()) || strings.Contains(text, "1 2 3 4") || strings.Contains(text, "01020304") {
				t.Fatal("native wire formatting leaked")
			}
		}
		var out bytes.Buffer
		slog.New(slog.NewJSONHandler(&out, nil)).Info("test", "value", value)
		if strings.Contains(out.String(), secret.wire()) {
			t.Fatal("native wire logging leaked")
		}
	}
	for _, value := range []any{wire, grant} {
		if s := value.(fmt.Stringer).String() + value.(fmt.GoStringer).GoString(); strings.Contains(s, secret.wire()) {
			t.Fatal("native wire interface leaked")
		}
	}
	encoded, err := json.Marshal(grant)
	if err != nil || !bytes.Contains(encoded, []byte(secret.wire())) {
		t.Fatal("explicit wire conversion failed")
	}
	for _, err := range []error{ErrNativeUnauthorized, ErrNativeLimit, nativeStoreFailure(context.Canceled), errors.New("private storage detail"), httpx.ErrForbidden("denied")} {
		converted := nativeHandlerError(err)
		if strings.Contains(converted.Error(), "private") {
			t.Fatal("native handler error leaked")
		}
	}
}

func TestNativeLoginValidationAndSharedIPBudget(t *testing.T) {
	h := nativeUnitHandler(t)
	for _, body := range []string{`{}`, `{"username":"x","password":"x","client_instance_id":"bad"}`,
		`{"username":"x","password":"x","client_instance_id":"` + uuid.Nil.String() + `"}`,
		`{"username":"` + strings.Repeat("a", 65) + `","password":"x","client_instance_id":"bad"}`,
		`{"username":"x","password":"` + strings.Repeat("x", MaxPasswordBytes+1) + `","client_instance_id":"bad"}`} {
		nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", body), http.StatusBadRequest)
	}
	// A browser request spends the first of the very same route budget.
	h.accounts.loginPerIP = httpx.NewRateLimiter(2, time.Hour)
	shared := h.accounts.loginPerIP.PerIP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	shared.ServeHTTP(httptest.NewRecorder(), nativeRequest(http.MethodPost, "/browser-login", ""))
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", "{}"), http.StatusBadRequest)
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", "{}"), http.StatusTooManyRequests)
	nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", "{}"), http.StatusBadRequest)
}
