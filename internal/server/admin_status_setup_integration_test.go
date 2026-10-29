//go:build integration

package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/setupcheck"
)

func setupCheckByID(t *testing.T, st OperatorStatus, id string) setupcheck.Check {
	t.Helper()
	for _, c := range st.Setup {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", id, st.Setup)
	return setupcheck.Check{}
}

func TestAdminStatusSetupChecklistNamesTheFix(t *testing.T) {
	rt := &setupcheck.Runtime{}
	rt.SetAnnounce(setupcheck.SourceDetected, []string{"203.0.113.7"}, "")
	a := newAppWithDeps(t, false, func(c *config.Config) {
		// Opened through the test server's address, but configured for another one.
		c.PublicURL = "http://chat.example.com"
	}, func(d *Deps) {
		d.Setup = rt
		d.StorageReady = func() error { return errors.New("seaweedfs:8333 refused") }
	})
	owner := a.seedAdmin()

	res := owner.get("/api/admin/status")
	if res.status != http.StatusOK {
		t.Fatalf("status: %d %s", res.status, res.body)
	}
	var st OperatorStatus
	res.decode(t, &st)

	if c := setupCheckByID(t, st, "public_url"); c.Level != setupcheck.Warn || !strings.Contains(c.Fix, "PUBLIC_URL="+a.origin()) {
		t.Errorf("public_url = %+v", c)
	}
	if c := setupCheckByID(t, st, "storage"); c.Level != setupcheck.Fail || !strings.Contains(c.Fix, "S3_ACCESS_KEY") || strings.Contains(c.Detail, "seaweedfs:8333") {
		t.Errorf("storage = %+v", c)
	}
	if c := setupCheckByID(t, st, "database"); c.Level != setupcheck.OK {
		t.Errorf("database = %+v", c)
	}
	if c := setupCheckByID(t, st, "voice_address"); c.Level != setupcheck.OK {
		t.Errorf("voice_address (development) = %+v", c)
	}
	if c := setupCheckByID(t, st, "mail"); c.Level != setupcheck.Warn || !strings.Contains(c.Fix, "SMTP_HOST") {
		t.Errorf("mail = %+v", c)
	}
	for _, secret := range []string{"postgres://", "JWT_SECRET="} {
		if strings.Contains(string(res.body), secret) {
			t.Errorf("status leaked %q: %s", secret, res.body)
		}
	}
}
