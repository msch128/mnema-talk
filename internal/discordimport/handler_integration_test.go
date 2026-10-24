//go:build integration

package discordimport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const importBase = "/api/admin/import/discord"

// httpWorld serves the import routes with the session replaced by a header
// that names who is calling (owner, admin, mod; none means signed out).
type httpWorld struct {
	*world
	h   *Handler
	mux http.Handler
}

func newHTTPWorld(t *testing.T) *httpWorld {
	t.Helper()
	w := newWorld(t)
	hw := &httpWorld{world: w, h: &Handler{Importer: w.im, Perms: w.svc}}
	users := map[string]auth.User{
		"owner": {ID: w.owner.ID, DisplayName: "Owner", Role: "admin"},
		"admin": {ID: w.admin.ID, DisplayName: "Deputy", Role: "user"},
		"mod":   {ID: w.mod.ID, DisplayName: "Helper", Role: "user"},
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			req.Body = http.MaxBytesReader(rw, req.Body, 4096)
			u, ok := users[req.Header.Get("X-As")]
			if !ok {
				httpx.WriteError(rw, httpx.ErrUnauthorized("no session"))
				return
			}
			next.ServeHTTP(rw, req.WithContext(auth.WithUser(req.Context(), &u)))
		})
	})
	r.Route("/api/admin", hw.h.MountAdmin)
	hw.mux = r
	return hw
}

type httpResult struct {
	status int
	body   []byte
}

func (hw *httpWorld) do(as, method, path, body string) httpResult {
	hw.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("X-As", as)
	rec := httptest.NewRecorder()
	hw.mux.ServeHTTP(rec, req)
	return httpResult{rec.Code, rec.Body.Bytes()}
}

func (hw *httpWorld) apply(as string, req ApplyRequest) httpResult {
	return hw.do(as, "POST", importBase+"/apply", hw.json(req))
}

func (hw *httpWorld) json(v any) string {
	hw.t.Helper()
	b, err := json.Marshal(v)
	hw.must(err)
	return string(b)
}

func (hw *httpWorld) previewPlan(as string) *Plan {
	hw.t.Helper()
	res := hw.do(as, "POST", importBase+"/preview", `{"template":"https://discord.new/AbCdEf123456"}`)
	if res.status != http.StatusOK {
		hw.t.Fatalf("preview: %d %s", res.status, res.body)
	}
	var plan Plan
	hw.must(json.Unmarshal(res.body, &plan))
	return &plan
}

func (hw *httpWorld) applied(as string) Record {
	hw.t.Helper()
	res := hw.apply(as, applyReq(hw.previewPlan(as)))
	if res.status != http.StatusCreated {
		hw.t.Fatalf("apply: %d %s", res.status, res.body)
	}
	var rec Record
	hw.must(json.Unmarshal(res.body, &rec))
	return rec
}

func TestImportRoutesAdmitOnlyAdministrators(t *testing.T) {
	hw := newHTTPWorld(t)
	missing := importBase + "/imports/" + uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{"POST", importBase + "/preview"},
		{"POST", importBase + "/apply"},
		{"GET", importBase + "/imports"},
		{"GET", missing},
		{"POST", missing + "/undo"},
	} {
		if res := hw.do("", rt.method, rt.path, `{}`); res.status != http.StatusUnauthorized {
			t.Errorf("signed out %s %s: %d", rt.method, rt.path, res.status)
		}
		if res := hw.do("mod", rt.method, rt.path, `{}`); res.status != http.StatusForbidden {
			t.Errorf("manager %s %s: %d", rt.method, rt.path, res.status)
		}
	}
	if n := hw.tables()["discord_imports"]; n != 0 {
		t.Fatalf("%d imports recorded by refused calls", n)
	}
}

func TestPreviewAndApplyRefuseBadBodies(t *testing.T) {
	hw := newHTTPWorld(t)
	big := `{"template":"` + strings.Repeat("a", 5000) + `"}`
	cases := []struct {
		name, path, body string
		status           int
	}{
		{"preview empty body", "/preview", ``, http.StatusBadRequest},
		{"preview malformed", "/preview", `{"template":`, http.StatusBadRequest},
		{"preview unknown field", "/preview", `{"template":"AbCdEf123456","x":1}`, http.StatusBadRequest},
		{"preview foreign host", "/preview", `{"template":"https://evil.example/template/AbCdEf123456"}`, http.StatusBadRequest},
		{"preview too large", "/preview", big, http.StatusRequestEntityTooLarge},
		{"apply empty body", "/apply", ``, http.StatusBadRequest},
		{"apply unknown field", "/apply", `{"template":"AbCdEf123456","evil":true}`, http.StatusBadRequest},
		{"apply too large", "/apply", big, http.StatusRequestEntityTooLarge},
		{"apply no confirmations", "/apply", `{"template":"AbCdEf123456","plan_hash":"x"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		if res := hw.do("admin", "POST", importBase+c.path, c.body); res.status != c.status {
			t.Errorf("%s: %d %s, want %d", c.name, res.status, res.body, c.status)
		}
	}
	if n := hw.tables()["discord_imports"]; n != 0 {
		t.Fatalf("%d imports recorded", n)
	}
}

func TestPreviewAndApplyAreRateLimitedPerMember(t *testing.T) {
	hw := newHTTPWorld(t)
	hw.h.Limit = httpx.NewRateLimiter(2, time.Minute)
	body := `{"template":"AbCdEf123456"}`
	for i := 0; i < 2; i++ {
		if res := hw.do("admin", "POST", importBase+"/preview", body); res.status != http.StatusOK {
			t.Fatalf("preview %d: %d %s", i, res.status, res.body)
		}
	}
	if res := hw.do("admin", "POST", importBase+"/preview", body); res.status != http.StatusTooManyRequests {
		t.Fatalf("third preview: %d", res.status)
	}
	if res := hw.do("admin", "POST", importBase+"/apply", body); res.status != http.StatusTooManyRequests {
		t.Fatalf("apply shares the budget: %d", res.status)
	}
	// Another member has a budget of their own.
	if res := hw.do("owner", "POST", importBase+"/preview", body); res.status != http.StatusOK {
		t.Fatalf("owner: %d %s", res.status, res.body)
	}
}

func TestApplyOverHTTPRecordsListsAndDetailsTheImport(t *testing.T) {
	hw := newHTTPWorld(t)
	stale := applyReq(hw.previewPlan("admin"))
	stale.PlanHash = "deadbeef"
	if res := hw.apply("admin", stale); res.status != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", res.status, res.body)
	}
	rec := hw.applied("admin")
	if rec.Counts.Channels != 7 || rec.Counts.Roles != 3 {
		t.Fatalf("record = %+v", rec)
	}

	var list []Record
	res := hw.do("owner", "GET", importBase+"/imports", "")
	hw.must(json.Unmarshal(res.body, &list))
	if res.status != http.StatusOK || len(list) != 1 || list[0].ID != rec.ID {
		t.Fatalf("list = %d %s", res.status, res.body)
	}
	var d Detail
	res = hw.do("owner", "GET", importBase+"/imports/"+rec.ID.String(), "")
	hw.must(json.Unmarshal(res.body, &d))
	if res.status != http.StatusOK || d.Remaining != rec.Counts {
		t.Fatalf("detail = %d %s", res.status, res.body)
	}
}

func TestDetailAndUndoRefuseUnknownAndMalformedIDs(t *testing.T) {
	hw := newHTTPWorld(t)
	unknown := importBase + "/imports/" + uuid.NewString()
	bad := importBase + "/imports/not-a-uuid"
	for _, c := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"detail unknown", "GET", unknown, "", http.StatusNotFound},
		{"detail malformed", "GET", bad, "", http.StatusBadRequest},
		{"undo unknown", "POST", unknown + "/undo", `{}`, http.StatusNotFound},
		{"undo malformed id", "POST", bad + "/undo", `{}`, http.StatusBadRequest},
		{"undo empty body", "POST", unknown + "/undo", ``, http.StatusBadRequest},
		{"undo unknown field", "POST", unknown + "/undo", `{"x":1}`, http.StatusBadRequest},
	} {
		if res := hw.do("admin", c.method, c.path, c.body); res.status != c.status {
			t.Errorf("%s: %d %s, want %d", c.name, res.status, res.body, c.status)
		}
	}
}

func TestUndoOverHTTPNeedsMessageLossAcceptanceAndRunsOnce(t *testing.T) {
	hw := newHTTPWorld(t)
	rec := hw.applied("admin")
	_, err := hw.p.Exec(t.Context(), `INSERT INTO messages (channel_id, user_id, content) SELECT c.id, $1, 'hi' FROM channels c WHERE c.name = 'rules'`, hw.admin.ID)
	hw.must(err)
	path := importBase + "/imports/" + rec.ID.String() + "/undo"

	if res := hw.do("admin", "POST", path, `{}`); res.status != http.StatusConflict {
		t.Fatalf("undo with messages: %d %s", res.status, res.body)
	}
	res := hw.do("admin", "POST", path, hw.json(UndoRequest{AcceptMessageLoss: true}))
	var out UndoResult
	hw.must(json.Unmarshal(res.body, &out))
	if res.status != http.StatusOK || out.Removed != rec.Counts {
		t.Fatalf("undo = %d %s", res.status, res.body)
	}
	if res := hw.do("admin", "POST", path, `{}`); res.status != http.StatusConflict {
		t.Fatalf("second undo: %d", res.status)
	}
}
