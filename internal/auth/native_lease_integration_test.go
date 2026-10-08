//go:build integration

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type nativeCommitObserver struct {
	t     *testing.T
	check func(uuid.UUID)
	calls int
}

func TestNativeGrantWireMetadataIsAuthoritativeAndBounded(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	for sequence := uint64(0); sequence < 2; sequence++ {
		w := httptest.NewRecorder()
		writeNativeGrant(w, grant)
		var wire struct {
			Family   uuid.UUID `json:"family_id"`
			Instance uuid.UUID `json:"client_instance_id"`
			Sequence uint64    `json:"refresh_sequence"`
		}
		if json.Unmarshal(w.Body.Bytes(), &wire) != nil || wire.Family != grant.Principal().FamilyID() || wire.Instance != grant.Principal().ClientInstanceID() || wire.Sequence != sequence {
			t.Fatal("wire metadata was not server family provenance")
		}
		if sequence == 0 {
			nativeReady(t, pool, grant)
			next, err := service.RotateRefresh(context.Background(), grant.refresh.wire())
			if err != nil {
				t.Fatal(err)
			}
			grant = next
		}
	}
	for _, invalid := range []IssuedNative{{}, func() IssuedNative { copy := grant; copy.refreshSequence = nativeMaxSequence + 1; return copy }(), func() IssuedNative { copy := grant; copy.principal.instanceID = uuid.Nil; return copy }()} {
		w := httptest.NewRecorder()
		writeNativeGrant(w, invalid)
		if w.Code != http.StatusInternalServerError {
			t.Fatal("invalid grant identity/sequence emitted")
		}
		var body map[string]any
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["access_token"] != nil || body["refresh_token"] != nil {
			t.Fatal("invalid wire grant emitted credentials")
		}
	}
}

func (o *nativeCommitObserver) DisconnectNativeFamily(id uuid.UUID) { o.calls++; o.check(id) }

func TestNativeLeaseUsesExactAccessBindingAndExpiry(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	lease, err := service.AuthenticateAccessLease(context.Background(), grant.access.wire())
	if err != nil || !lease.Principal().SameNativeAccess(grant.Principal()) || lease.Deadline().After(time.Now().Add(NativeLeaseMaximum)) || !lease.Deadline().Before(lease.AccessDeadline()) {
		t.Fatal("native lease binding/deadline invalid")
	}
	if !lease.Principal().FamilyExpiresAt().Equal(grant.FamilyExpiresAt()) {
		t.Fatal("lease did not preserve the authenticated family expiry")
	}
	fresh, err := service.RevalidateNativeLease(context.Background(), lease.Principal())
	if err != nil || !fresh.Principal().SameNativeAccess(lease.Principal()) {
		t.Fatal("valid native lease revalidation failed")
	}
	wrong := lease.Principal()
	wrong.instanceID = uuid.New()
	if _, err := service.RevalidateNativeLease(context.Background(), wrong); err != ErrNativeUnauthorized {
		t.Fatal("foreign instance revalidated")
	}
	wrong = lease.Principal()
	wrong.familyExpiresAt = wrong.familyExpiresAt.Add(time.Second)
	if _, err := service.RevalidateNativeLease(context.Background(), wrong); err != ErrNativeUnauthorized {
		t.Fatal("modified family expiry revalidated")
	}
	if _, err := service.RevalidateNativeLease(context.Background(), NativePrincipal{}); err != ErrNativeUnauthorized {
		t.Fatal("missing principal revalidated")
	}
	if _, err := service.AuthenticateAccessLease(context.Background(), "invalid"); err != ErrNativeUnauthorized {
		t.Fatal("malformed lease token accepted")
	}
	if _, err := pool.Exec(context.Background(), `UPDATE native_access_tokens SET expires_at=clock_timestamp()+interval '2 seconds' WHERE family_id=$1`, grant.Principal().FamilyID()); err != nil {
		t.Fatal(err)
	}
	short, err := service.AuthenticateAccessLease(context.Background(), grant.access.wire())
	if err != nil || short.Deadline().After(time.Now().Add(2*time.Second)) || !short.Deadline().Equal(short.AccessDeadline()) {
		t.Fatal("access expiration did not clamp lease")
	}
	if _, err := service.RevalidateNativeLease(context.Background(), lease.Principal()); err != ErrNativeUnauthorized {
		t.Fatal("changed access row adopted by old principal")
	}
	if _, err := makeNativeLease(time.Now().Add(-time.Minute), time.Now(), grant.Principal()); err != ErrNativeUnauthorized {
		t.Fatal("delayed completion produced a new lease")
	}
	if _, err := service.RevalidateNativeLease(context.Background(), short.Principal()); err != nil {
		t.Fatal("new short principal invalid")
	}
	if err := service.RevokeFamily(context.Background(), short.Principal()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RevalidateNativeLease(context.Background(), short.Principal()); err != ErrNativeUnauthorized {
		t.Fatal("revoked family revalidated")
	}
}

func TestNativeLeaseAdmissionRejectsBrowserTargets(t *testing.T) {
	_, user, handler := nativeHTTPFixture(t)
	grant := nativeHTTPLogin(t, handler, user)
	for _, boundary := range []string{"origin", "cookie", "query", "userinfo", "fragment"} {
		t.Run(boundary, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/native/socket", nil)
			request.Header.Set("Authorization", "Bearer "+grant.Access)
			switch boundary {
			case "origin":
				request.Header.Set("Origin", "https://community.example.invalid")
			case "cookie":
				request.AddCookie(&http.Cookie{Name: "mnema_session", Value: "synthetic-browser-cookie"})
			case "query":
				request.URL.RawQuery = "target=other"
			case "userinfo":
				request.URL.User = url.User("synthetic-user")
			case "fragment":
				request.URL.Fragment = "other-target"
			}
			lease, err := handler.AuthenticateNativeRequest(request)
			var apiError *httpx.APIError
			if !errors.As(err, &apiError) || apiError.Status != http.StatusForbidden || lease != (NativeLease{}) {
				t.Fatal("browser-controlled target acquired native admission")
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, "/native/socket", nil)
	request.Header.Set("Authorization", "Bearer "+grant.Access)
	if lease, err := handler.AuthenticateNativeRequest(request); err != nil || lease.Principal().FamilyID() == uuid.Nil {
		t.Fatal("rejected targets invalidated the original native family")
	}
	request = httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"unterminated`))
	request.Header.Set("Content-Type", "application/json")
	values, err := decodeNativeStrings(httptest.NewRecorder(), request, "username")
	var apiError *httpx.APIError
	if !errors.As(err, &apiError) || apiError.Status != http.StatusBadRequest || values != nil {
		t.Fatal("malformed JSON key was accepted as native credentials")
	}
}

func TestNativeFamilyObserverRunsOnlyAfterConfirmedCommit(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	o := &nativeCommitObserver{t: t, check: func(id uuid.UUID) {
		var revoked bool
		if err := pool.QueryRow(context.Background(), `SELECT revoked_at IS NOT NULL FROM native_session_families WHERE id=$1`, id).Scan(&revoked); err != nil || !revoked {
			t.Fatal("observer preceded actual committed revocation")
		}
	}}
	if err := service.BindNativeFamilyControl(nil); err == nil {
		t.Fatal("nil observer accepted")
	}
	if err := service.BindNativeFamilyControl(o); err != nil {
		t.Fatal(err)
	}
	if err := service.BindNativeFamilyControl(o); err == nil {
		t.Fatal("observer could be rebound")
	}
	nativeReady(t, pool, grant)
	if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != nil {
		t.Fatal(err)
	}
	if o.calls != 0 {
		t.Fatal("ordinary refresh notified revoke")
	}
	if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); err != ErrNativeUnauthorized || o.calls != 1 {
		t.Fatal("committed reuse did not notify exactly once before auth error")
	}
	other := nativeIssue(t, service, proof)
	if err := service.RevokeFamily(context.Background(), other.Principal()); err != nil || o.calls != 2 {
		t.Fatal("committed logout did not notify")
	}
	rollback := nativeIssue(t, service, proof)
	fault := nativeFaultService(t, pool, &authQueryHook{match: "UPDATE native_session_families SET revoked_at", cancel: true})
	if err := fault.BindNativeFamilyControl(o); err != nil {
		t.Fatal(err)
	}
	if err := fault.RevokeFamily(context.Background(), rollback.Principal()); err == nil || o.calls != 2 {
		t.Fatal("rollback notified successful revoke")
	}
	if _, err := service.AuthenticateAccessLease(context.Background(), rollback.access.wire()); err != nil {
		t.Fatal("rollback invalidated access")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.AuthenticateAccessLease(ctx, rollback.access.wire()); !errors.Is(err, context.Canceled) {
		t.Fatal("lease lost cancellation cause")
	}
}
