//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func newRegistrationTestApp(t *testing.T, compatibility NativeCompatibility) *previewTestApp {
	t.Helper()
	auth.SetPasswordCostForTests(4)
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	server := httptest.NewUnstartedServer(nil)
	origin := "https://" + server.Listener.Addr().String()
	cfg, err := config.FromEnv(func(key string) (string, bool) {
		value, ok := map[string]string{"DATABASE_URL": "unused", "PUBLIC_URL": origin, "JWT_SECRET": "native-registration-synthetic-secret-123456", "MEDIA_RETENTION_DAYS": "0"}[key]
		return value, ok
	})
	if err != nil {
		t.Fatal("registration configuration")
	}
	router, err := NewNativeRegistrationPreviewRouter(Deps{Config: cfg, DB: pool, Version: "native-registration-fixture"}, NativePreviewOptions{CommunityID: "native-registration-fixture", InstanceOrigin: origin, Compatibility: compatibility})
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = router
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Cleanup(router.Close)
	if server.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS disabled")
	}
	a := &previewTestApp{server: server, router: router.NativePreviewRouter, pool: pool}
	a.admin = auth.User{ID: uuid.New(), Username: "native-signup-admin", Role: auth.RoleAdmin}
	hash, err := auth.HashPassword(nativePreviewFixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,role)VALUES($1,$2,$2,$3,'admin')`, a.admin.ID, a.admin.Username, hash); err != nil {
		t.Fatal(err)
	}
	return a
}
func signupInput(username, code string) map[string]string {
	return map[string]string{"username": username, "display_name": "新しい利用者", "password": nativePreviewFixturePassword, "invite_code": code}
}
func signupInvite(t *testing.T, a *previewTestApp, code string, max int) {
	t.Helper()
	if _, err := auth.CreateInvite(context.Background(), a.pool, a.admin.ID, code, &max, nil); err != nil {
		t.Fatal(err)
	}
}
func registrationResponse(t *testing.T, a *previewTestApp, path string, body any, headers http.Header) (int, []byte, http.Header) {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, a.server.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for name, values := range headers {
		request.Header[name] = values
	}
	response, err := a.server.Client().Do(request)
	if err != nil {
		t.Fatal("actual registration TLS request failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data, response.Header
}
func TestNativeRegistrationActualInviteThenSeparateLogin(t *testing.T) {
	a := newRegistrationTestApp(t, NativeSupported)
	signupInvite(t, a, "native-invite-one", 1)
	status, data, headers := registrationResponse(t, a, NativePreviewPrefix+"/auth/register", signupInput(" native-new-user ", "native-invite-one"), nil)
	if status != http.StatusCreated || len(headers.Values("Set-Cookie")) != 0 || headers.Get("Cache-Control") != "no-store" {
		t.Fatal("native account creation boundary")
	}
	var envelope auth.UserEnvelope
	var wire map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil || json.Unmarshal(data, &wire) != nil || len(wire) != 1 || envelope.User.Username != "native-new-user" || envelope.User.Role != "user" || envelope.User.DisplayName != "新しい利用者" {
		t.Fatal("real user-only signup response")
	}
	var families, uses int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_session_families`).Scan(&families) != nil || families != 0 {
		t.Fatal("registration minted session")
	}
	if a.pool.QueryRow(context.Background(), `SELECT uses_count FROM invites WHERE code='native-invite-one'`).Scan(&uses) != nil || uses != 1 {
		t.Fatal("invite consumption")
	}
	a.request(t, http.MethodGet, NativePreviewPrefix+"/auth/me", nil, "", nil, http.StatusUnauthorized)
	grant := a.login(t, envelope.User)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/auth/me", nil, grant.Access, nil, http.StatusOK)
	status, _, _ = registrationResponse(t, a, NativePreviewPrefix+"/auth/register", signupInput("another-user", "native-invite-one"), nil)
	if status == http.StatusCreated {
		t.Fatal("consumed invitation reused")
	}
	status, _, headers = registrationResponse(t, a, "/api/auth/register", signupInput("browser-new", "unknown-invite"), http.Header{"Origin": []string{a.server.URL}})
	if status == http.StatusCreated || len(headers.Values("Set-Cookie")) != 0 {
		t.Fatal("browser invite policy changed")
	}
}
func TestNativeRegistrationSharesActualBrowserIPBudget(t *testing.T) {
	a := newRegistrationTestApp(t, NativeSupported)
	for i := 0; i < 9; i++ {
		status, _, _ := registrationResponse(t, a, "/api/auth/register", signupInput("no-invite-user", "unknown-invite"), http.Header{"Origin": []string{a.server.URL}})
		if status == http.StatusTooManyRequests || status == http.StatusCreated {
			t.Fatal("browser pre-budget result")
		}
	}
	status, _, headers := registrationResponse(t, a, NativePreviewPrefix+"/auth/register", signupInput("no-invite-user", "unknown-invite"), nil)
	if status == http.StatusTooManyRequests || len(headers.Values("Set-Cookie")) != 0 {
		t.Fatal("last shared budget attempt")
	}
	status, _, headers = registrationResponse(t, a, NativePreviewPrefix+"/auth/register", signupInput("no-invite-user", "unknown-invite"), nil)
	if status != http.StatusTooManyRequests || headers.Get("Retry-After") == "" {
		t.Fatal("native registration bypassed browser IP budget")
	}
}
func TestNativeRegistrationStrictActualHTTPSAndUnsupported(t *testing.T) {
	a := newRegistrationTestApp(t, NativeSupported)
	signupInvite(t, a, "native-invite-strict", 10)
	for _, headers := range []http.Header{{"Origin": []string{""}}, {"Cookie": []string{""}}, {"Authorization": []string{""}}} {
		status, _, reply := registrationResponse(t, a, NativePreviewPrefix+"/auth/register", signupInput("never-created", "native-invite-strict"), headers)
		if status < 400 || len(reply.Values("Set-Cookie")) != 0 {
			t.Fatal("foreign credential header accepted")
		}
	}
	for _, suffix := range []string{"?", "?token=synthetic"} {
		status, _, _ := registrationResponse(t, a, NativePreviewPrefix+"/auth/register"+suffix, signupInput("never-created", "native-invite-strict"), nil)
		if status != http.StatusForbidden {
			t.Fatal("native query accepted")
		}
	}
	var users int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE username='never-created'`).Scan(&users) != nil || users != 0 {
		t.Fatal("rejected boundary mutated user")
	}
	t.Run("unsupported", func(t *testing.T) {
		blocked := newRegistrationTestApp(t, NativeUnsupported)
		status, _, _ := registrationResponse(t, blocked, NativePreviewPrefix+"/auth/register", signupInput("never-created", "native-invite-strict"), nil)
		if status != http.StatusServiceUnavailable {
			t.Fatal("unsupported native registration admitted")
		}
	})
}
func TestNativeRegistrationConcurrentNativeBrowserSingleUse(t *testing.T) {
	a := newRegistrationTestApp(t, NativeSupported)
	signupInvite(t, a, "native-browser-race", 1)
	var wait sync.WaitGroup
	statuses := make(chan int, 2)
	for _, browser := range []bool{false, true} {
		wait.Add(1)
		go func(browser bool) {
			defer wait.Done()
			path := NativePreviewPrefix + "/auth/register"
			name := "native-race"
			headers := http.Header{}
			if browser {
				path = "/api/auth/register"
				name = "browser-race"
				headers.Set("Origin", a.server.URL)
			}
			payload, _ := json.Marshal(signupInput(name, "native-browser-race"))
			req, _ := http.NewRequest(http.MethodPost, a.server.URL+path, bytes.NewReader(payload))
			req.Header = headers
			req.Header.Set("Content-Type", "application/json")
			response, err := a.server.Client().Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			defer response.Body.Close()
			_, _ = io.Copy(io.Discard, response.Body)
			if !browser && len(response.Cookies()) != 0 {
				statuses <- 0
				return
			}
			statuses <- response.StatusCode
		}(browser)
	}
	wait.Wait()
	close(statuses)
	created := 0
	for status := range statuses {
		if status == http.StatusCreated {
			created++
		} else if status < 400 {
			t.Fatal("race transport or status failure")
		}
	}
	if created != 1 {
		t.Fatal("single-use invite did not admit exactly one account")
	}
	var count, uses int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE username IN ('native-race','browser-race')`).Scan(&count) != nil || count != 1 {
		t.Fatal("registration race created extra users")
	}
	if a.pool.QueryRow(context.Background(), `SELECT uses_count FROM invites WHERE code='native-browser-race'`).Scan(&uses) != nil || uses != 1 {
		t.Fatal("invite race consumed incorrectly")
	}
}
func TestNativeRegistrationDatabaseFaultSanitized(t *testing.T) {
	a := newRegistrationTestApp(t, NativeSupported)
	// Use an actual failing registration transaction rather than simulated success.
	signupInvite(t, a, "native-fault-invite", 1)
	if _, err := a.pool.Exec(context.Background(), `CREATE OR REPLACE FUNCTION native_registration_fault() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic-sensitive-invite-password-detail';END$$; CREATE TRIGGER native_registration_fault_trigger BEFORE INSERT ON users FOR EACH ROW EXECUTE FUNCTION native_registration_fault()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = a.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS native_registration_fault_trigger ON users;DROP FUNCTION IF EXISTS native_registration_fault()`)
	})
	status, data, headers := registrationResponse(t, a, NativePreviewPrefix+"/auth/register", signupInput("native-fault-user", "native-fault-invite"), nil)
	if status != http.StatusInternalServerError || strings.Contains(string(data), "synthetic-sensitive") || len(headers.Values("Set-Cookie")) != 0 {
		t.Fatal("database failure leaked or signed in")
	}
	var uses int
	if a.pool.QueryRow(context.Background(), `SELECT uses_count FROM invites WHERE code='native-fault-invite'`).Scan(&uses) != nil || uses != 0 {
		t.Fatal("failed transaction consumed invite")
	}
}

func TestNativeRegistrationPreservesPasswordBytesForSeparateLogin(t *testing.T) {
	a := newRegistrationTestApp(t, NativeSupported)
	signupInvite(t, a, "native-password-bytes", 1)
	input := signupInput("native-password-user", "native-password-bytes")
	input["password"] = "  native-synthetic-password\n"
	status, _, headers := registrationResponse(t, a, NativePreviewPrefix+"/auth/register", input, nil)
	if status != http.StatusCreated || len(headers.Values("Set-Cookie")) != 0 {
		t.Fatal("native password signup failed")
	}
	login := map[string]string{"username": input["username"], "password": strings.TrimSpace(input["password"]), "client_instance_id": uuid.NewString()}
	status, _, _ = registrationResponse(t, a, NativePreviewPrefix+"/auth/login", login, nil)
	if status != http.StatusUnauthorized {
		t.Fatal("registration trimmed password bytes")
	}
	login["password"] = input["password"]
	status, data, headers := registrationResponse(t, a, NativePreviewPrefix+"/auth/login", login, nil)
	var grant previewTestGrant
	if status != http.StatusOK || len(headers.Values("Set-Cookie")) != 0 || json.Unmarshal(data, &grant) != nil || grant.Family == uuid.Nil || grant.User.Username != input["username"] {
		t.Fatal("separate login did not preserve original password")
	}
	a.request(t, http.MethodGet, NativePreviewPrefix+"/auth/me", nil, grant.Access, nil, http.StatusOK)
	a.request(t, http.MethodPost, NativePreviewPrefix+"/auth/logout", nil, grant.Access, nil, http.StatusNoContent)
}
