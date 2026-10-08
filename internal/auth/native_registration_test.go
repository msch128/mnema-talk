package auth

import (
	"net/http"
	"strings"
	"testing"
)

func TestNativeRegistrationDependencies(t *testing.T) {
	for _, h := range []*NativeHandler{nil, {}, {accounts: &Handler{}}, {accounts: &Handler{Sessions: &Sessions{}}}} {
		if _, err := h.RegistrationHandler(); err == nil {
			t.Fatal("invalid registration dependencies accepted")
		}
	}
	h := nativeUnitHandler(t)
	h.accounts.registerPerIP = nil
	if _, err := h.RegistrationHandler(); err == nil {
		t.Fatal("missing shared registration budget accepted")
	}
}
func TestNativeRegistrationStrictBoundaryWithoutDatabase(t *testing.T) {
	h := nativeUnitHandler(t)
	handler, err := h.RegistrationHandler()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Origin", "oRiGiN", "Cookie", "Authorization", "aUtHoRiZaTiOn"} {
		request := nativeRequest(http.MethodPost, "/auth/register", "{}")
		request.Header[name] = []string{""}
		want := http.StatusForbidden
		if strings.EqualFold(name, "Authorization") {
			want = http.StatusBadRequest
		}
		nativeStatus(t, handler, request, want)
	}
	for _, path := range []string{"/auth/register?", "/auth/register?invite=synthetic", "/auth/%72egister"} {
		nativeStatus(t, handler, nativeRequest(http.MethodPost, path, "{}"), http.StatusBadRequest)
	}
	for _, body := range []string{"{}", "null", "[]", `{"username":"a","username":"b","display_name":"","password":"synthetic-password","invite_code":"synthetic"}`, `{"username":"bad!","display_name":"","password":"synthetic-password","invite_code":"synthetic"}`, `{"username":"new-user","display_name":"","password":null,"invite_code":"synthetic"}`, `{"username":"new-user","display_name":"","password":"synthetic-password","invite_code":"synthetic","approved":true}`, `{"username":"new-user","display_name":"","password":"synthetic-password","invite_code":"synthetic"} {}`} {
		nativeStatus(t, handler, nativeRequest(http.MethodPost, "/auth/register", body), http.StatusBadRequest)
	}
	request := nativeRequest(http.MethodPost, "/auth/register", strings.Repeat("x", int(nativeAuthMaxBody)+1))
	nativeStatus(t, handler, request, http.StatusRequestEntityTooLarge)
	nativeStatus(t, handler, nativeRequest(http.MethodGet, "/auth/register", ""), http.StatusMethodNotAllowed)
	nativeStatus(t, handler, nativeRequest(http.MethodPost, "/unknown", "{}"), http.StatusNotFound)
}
