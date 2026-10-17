//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/perm"
)

func setGlobalLimit(t *testing.T, c *client, mb any, want int) {
	t.Helper()
	if res := c.put("/api/admin/upload-limit", map[string]any{"max_upload_mb": mb}); res.status != want {
		t.Fatalf("set limit %v: %d %s", mb, res.status, res.body)
	}
}

func TestUploadLimitResolutionAndEnforcement(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.MaxUploadMB = 4 })
	owner := a.seedAdmin()
	member := a.register(owner, "alice")
	ch := a.createChannel(owner, "general", "text")
	path := "/api/channels/" + ch.String() + "/upload"
	mb := 1 << 20

	// Default: the env value is both ceiling and global limit.
	if got := myPerms(t, member).MaxUploadMB; got != 4 {
		t.Fatalf("default limit %d, want 4", got)
	}
	if res := member.uploadParts(path, pngFile(3*mb)); res.status != http.StatusCreated {
		t.Fatalf("3 MB under a 4 MB limit: %d %s", res.status, res.body)
	}

	// The Owner lowers the limit: the member is refused with a clear message,
	// the Owner keeps the ceiling.
	setGlobalLimit(t, owner, 1, http.StatusOK)
	if got := myPerms(t, member).MaxUploadMB; got != 1 {
		t.Fatalf("lowered limit %d, want 1", got)
	}
	if got := myPerms(t, owner).MaxUploadMB; got != 4 {
		t.Fatalf("owner limit %d, want the ceiling 4", got)
	}
	res := member.uploadParts(path, pngFile(2*mb))
	if res.status != http.StatusRequestEntityTooLarge || !strings.Contains(string(res.body), "1 MB limit") {
		t.Fatalf("2 MB over a 1 MB limit: %d %s", res.status, res.body)
	}
	if res := owner.uploadParts(path, pngFile(2*mb)); res.status != http.StatusCreated {
		t.Fatalf("owner at 2 MB: %d %s", res.status, res.body)
	}
	// Huge bodies are cut at the per-request transport cap.
	if res := member.uploadParts(path, pngFile(3*mb)); res.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("3 MB over a 1 MB limit: %d %s", res.status, res.body)
	}

	// Role overrides: the highest one wins, the ceiling caps it.
	small := createRole(t, owner, "small", perm.DefaultEveryone)
	big := createRole(t, owner, "big", perm.DefaultEveryone)
	for id, v := range map[string]int{small.String(): 2, big.String(): 3} {
		if res := owner.patch("/api/admin/roles/"+id, map[string]any{"max_upload_mb": v}); res.status != http.StatusOK {
			t.Fatalf("role override: %d %s", res.status, res.body)
		}
	}
	assignRoles(t, owner, member, small)
	if got := myPerms(t, member).MaxUploadMB; got != 2 {
		t.Fatalf("one role %d, want 2", got)
	}
	assignRoles(t, owner, member, small, big)
	if got := myPerms(t, member).MaxUploadMB; got != 3 {
		t.Fatalf("max over roles %d, want 3", got)
	}
	if res := member.uploadParts(path, pngFile(2*mb)); res.status != http.StatusCreated {
		t.Fatalf("2 MB with a 3 MB role: %d %s", res.status, res.body)
	}
	if res := member.uploadParts(path, pngFile(3*mb+1)); res.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("over the role limit: %d %s", res.status, res.body)
	}
	// null inherits again.
	if res := owner.patch("/api/admin/roles/"+big.String(), map[string]any{"max_upload_mb": nil}); res.status != http.StatusOK {
		t.Fatalf("inherit: %d %s", res.status, res.body)
	}
	if got := myPerms(t, member).MaxUploadMB; got != 2 {
		t.Fatalf("after inherit %d, want 2", got)
	}

	// Validation and authority.
	setGlobalLimit(t, owner, 5, http.StatusBadRequest)
	setGlobalLimit(t, owner, 0, http.StatusBadRequest)
	setGlobalLimit(t, member, 2, http.StatusForbidden)
	if res := member.get("/api/admin/upload-limit"); res.status != http.StatusForbidden {
		t.Fatalf("member read: %d", res.status)
	}
	if res := owner.patch("/api/admin/roles/"+big.String(), map[string]any{"max_upload_mb": 5}); res.status != http.StatusBadRequest {
		t.Fatalf("role over the ceiling: %d %s", res.status, res.body)
	}
	manager := a.register(owner, "bob")
	mgr := createRole(t, owner, "mgr", perm.DefaultEveryone|perm.ManageRoles)
	assignRoles(t, owner, manager, mgr)
	low := createRole(t, owner, "low", perm.DefaultEveryone) // created last = lowest, so mgr outranks it
	if res := manager.patch("/api/admin/roles/"+low.String(), map[string]any{"max_upload_mb": 3}); res.status != http.StatusForbidden {
		t.Fatalf("role manager setting a limit: %d %s", res.status, res.body)
	}
	if res := manager.patch("/api/admin/roles/"+low.String(), map[string]any{"name": "lower"}); res.status != http.StatusOK {
		t.Fatalf("role manager renaming keeps the limit: %d %s", res.status, res.body)
	}

	// Reading, malformed bodies and creating a role with a limit.
	if res := owner.get("/api/admin/upload-limit"); res.status != http.StatusOK || !strings.Contains(string(res.body), `"ceiling_mb":4`) {
		t.Fatalf("read: %d %s", res.status, res.body)
	}
	if res := owner.do(http.MethodPut, "/api/admin/upload-limit", strings.NewReader("{"), map[string]string{"Content-Type": "application/json"}); res.status != http.StatusBadRequest {
		t.Fatalf("malformed: %d", res.status)
	}
	if res := owner.post("/api/admin/roles", map[string]any{"name": "capped", "max_upload_mb": 2}); res.status != http.StatusCreated || !strings.Contains(string(res.body), `"max_upload_mb":2`) {
		t.Fatalf("create with limit: %d %s", res.status, res.body)
	}
	if res := owner.post("/api/admin/roles", map[string]any{"name": "toobig", "max_upload_mb": 9}); res.status != http.StatusBadRequest {
		t.Fatalf("create above the ceiling: %d", res.status)
	}
	if res := manager.post("/api/admin/roles", map[string]any{"name": "mine", "max_upload_mb": 2}); res.status != http.StatusForbidden {
		t.Fatalf("manager create with limit: %d", res.status)
	}

	// Back to the ceiling.
	setGlobalLimit(t, owner, nil, http.StatusOK)
	assignRoles(t, owner, member)
	if got := myPerms(t, member).MaxUploadMB; got != 4 {
		t.Fatalf("reset %d, want 4", got)
	}
}

func TestUploadLimitAppliesToWebhooks(t *testing.T) {
	a := newAppWithConfig(t, func(c *config.Config) { c.MaxUploadMB = 4 })
	owner := a.seedAdmin()
	ch := a.createChannel(owner, "alerts", "text")
	h := makeHook(t, owner, ch, "bot")
	a.router.Webhooks.SetLimits(1000, 1000)
	setGlobalLimit(t, owner, 1, http.StatusOK)

	// Webhooks have no role: they get the global limit.
	big := append(append([]byte{}, png1x1...), make([]byte, 2<<20)...)
	body, ct := multipartBody(t, map[string]any{"content": "x"}, map[string][]byte{"c.png": big})
	if res := a.exec(h.path, body, map[string]string{"Content-Type": ct}); res.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("webhook over the global limit: %d %s", res.status, res.body)
	}
	body, ct = multipartBody(t, map[string]any{"content": "x"}, map[string][]byte{"c.png": png1x1})
	if res := a.exec(h.path+"?wait=true", body, map[string]string{"Content-Type": ct}); res.status != http.StatusOK {
		t.Fatalf("webhook under the limit: %d %s", res.status, res.body)
	}
}
