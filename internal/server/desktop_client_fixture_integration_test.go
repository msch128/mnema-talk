//go:build integration

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// This explicit opt-in fixture serves the normal router and embedded Vue app.
// Its private ready file is for the local UI driver only, never an artifact.
func TestDesktopClientGUIFixture(t *testing.T) {
	dir := os.Getenv("MNEMA_DESKTOP_FIXTURE_DIR")
	if dir == "" {
		t.Skip("requires an owned disposable desktop GUI fixture")
	}
	nonce := os.Getenv("MNEMA_DESKTOP_FIXTURE_NONCE")
	fixtureVersion := os.Getenv("MNEMA_DESKTOP_FIXTURE_VERSION")
	fixtureRevision := os.Getenv("MNEMA_DESKTOP_FIXTURE_REVISION")
	dsn := os.Getenv("TEST_DATABASE_URL")
	u, err := url.Parse(dsn)
	if err != nil || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(fixtureRevision) || !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(fixtureVersion) || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(nonce) ||
		!filepath.IsAbs(dir) || filepath.Base(dir) != "mnema-auth-"+nonce ||
		u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Port() == "" ||
		u.Path != "/mnema_desktop_"+nonce || u.RawQuery != "sslmode=disable" ||
		u.Fragment != "" || u.User == nil || u.User.Username() != "desktop_fixture" {
		t.Fatal("refusing a database without fixture ownership")
	}
	owner, err := os.ReadFile(filepath.Join(dir, "pg-data", ".mnema-owner"))
	if err != nil || string(owner) != nonce {
		t.Fatal("fixture cluster ownership marker missing")
	}
	ctx := context.Background()
	probe, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("cannot connect to fixture database")
	}
	var database, user, address, dataDir, pgVersion string
	var tables int
	err = probe.QueryRow(ctx, `SELECT current_database(), current_user, inet_server_addr()::text,
		current_setting('data_directory'), current_setting('server_version')`).Scan(&database, &user, &address, &dataDir, &pgVersion)
	if err == nil {
		err = probe.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname = 'public'`).Scan(&tables)
	}
	probe.Close()
	if err != nil || database != "mnema_desktop_"+nonce || user != "desktop_fixture" ||
		address != "127.0.0.1" || !strings.EqualFold(filepath.Clean(dataDir), filepath.Join(dir, "pg-data")) || tables != 0 {
		t.Fatal("refusing a nonempty or unowned fixture database")
	}
	// Only now may the existing integration helper migrate the fresh database.
	pool := testutil.DB(t)
	a := &app{t: t, db: pool, store: media.NewMemoryStore()}
	a.srv = httptest.NewUnstartedServer(nil)
	origin := "https://" + a.srv.Listener.Addr().String()
	cfg, err := config.FromEnv(func(k string) (string, bool) {
		v, ok := map[string]string{"DATABASE_URL": dsn, "PUBLIC_URL": origin,
			"JWT_SECRET": "desktop-local-fixture-" + nonce, "MEDIA_RETENTION_DAYS": "0"}[k]
		return v, ok
	})
	if err != nil {
		t.Fatal("fixture configuration failed")
	}
	a.router, err = NewRouter(Deps{Config: cfg, DB: pool, Store: a.store, Version: fixtureVersion})
	if err != nil {
		t.Fatal("normal fixture router failed")
	}
	t.Cleanup(a.router.Close)
	var ready atomic.Bool
	var logins, logouts atomic.Int32
	var historyCompleted atomic.Int32
	var historySuppliedInbound atomic.Bool
	var historyPath, inbound string // published to handlers by ready.Store(true)
	a.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			a.router.ServeHTTP(w, r)
			return
		}
		isHistory := r.Method == http.MethodGet && r.URL.Path == historyPath
		isAuth := r.Method == http.MethodPost && (r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/logout")
		if !isHistory && !isAuth {
			a.router.ServeHTTP(w, r)
			return
		}
		response := httptest.NewRecorder()
		a.router.ServeHTTP(response, r)
		for name, values := range response.Header() {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		if isHistory && response.Code == http.StatusOK {
			if bytes.Contains(response.Body.Bytes(), []byte(inbound)) {
				historySuppliedInbound.Store(true)
			}
		}
		w.WriteHeader(response.Code)
		_, writeErr := w.Write(response.Body.Bytes())
		if isHistory && response.Code == http.StatusOK && writeErr == nil {
			historyCompleted.Add(1)
		}
		if r.URL.Path == "/api/auth/login" && response.Code == http.StatusOK {
			logins.Add(1)
		}
		if r.URL.Path == "/api/auth/logout" && response.Code == http.StatusNoContent {
			logouts.Add(1)
		}
	})
	certificate, root := desktopFixtureCertificate(t)
	a.srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	a.srv.StartTLS()
	t.Cleanup(a.srv.Close)
	write := func(name string, data any) {
		t.Helper()
		bytes, err := json.Marshal(data)
		if err != nil || os.WriteFile(filepath.Join(dir, name+".tmp"), bytes, 0o600) != nil {
			t.Fatal("fixture output failed")
		}
		if err := os.Rename(filepath.Join(dir, name+".tmp"), filepath.Join(dir, name)); err != nil {
			t.Fatal("fixture output failed")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "root.crt"), root, 0o600); err != nil {
		t.Fatal("fixture CA output failed")
	}
	anon := func() *client {
		jar, _ := cookiejar.New(nil)
		return &client{a: a, http: &http.Client{Jar: jar, Transport: a.srv.Client().Transport, Timeout: 10 * time.Second}}
	}
	password := desktopFixtureSecret(t)
	if err := auth.EnsureAdminUser(ctx, pool, "desktop-fixture-admin", password, false); err != nil {
		t.Fatal("fixture admin creation failed")
	}
	admin := anon()
	if res := admin.post("/api/auth/login", map[string]string{"username": "desktop-fixture-admin", "password": password}); res.status != http.StatusOK {
		t.Fatal("fixture admin login failed")
	}
	channel := a.createChannel(admin, "desktop-fixture-chat", "text")
	historyPath = "/api/channels/" + channel.String() + "/messages"
	invite := admin.post("/api/admin/invites", map[string]any{})
	if invite.status != http.StatusCreated {
		t.Fatal("fixture invite creation failed")
	}
	var code auth.Invite
	invite.decode(t, &code)
	memberPassword := desktopFixtureSecret(t)
	member := anon()
	registration := member.post("/api/auth/register", map[string]string{"username": "desktop-fixture-user", "password": memberPassword, "invite_code": code.Code})
	if registration.status != http.StatusCreated {
		t.Fatal("fixture member registration failed")
	}
	var registered struct{ User auth.User }
	registration.decode(t, &registered)
	outbound := "Desktop fixture outbound " + nonce
	inbound = "Desktop fixture inbound " + nonce
	ready.Store(true)
	write("READY.json", map[string]any{"origin": origin, "username": registered.User.Username,
		"password": memberPassword, "channel": "desktop-fixture-chat", "outbound": outbound, "inbound": inbound,
		"postgres_version": pgVersion, "nonce": nonce, "backend_revision": fixtureRevision, "normal_router": true, "media_store": "memory-test-only"})
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Minute)
	defer deadline.Stop()
	peerSent := false
	for {
		select {
		case <-deadline.C:
			t.Fatal("desktop fixture timed out")
		case <-ticker.C:
			var count int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE channel_id=$1 AND user_id=$2 AND content=$3`, channel, registered.User.ID, outbound).Scan(&count); err != nil {
				t.Fatal("fixture message verification failed")
			}
			if count == 1 && !peerSent {
				for _, online := range a.router.Hub.OnlineUserIDs() {
					if online == registered.User.ID && historyCompleted.Load() > 0 {
						a.send(admin, channel, inbound)
						peerSent = true
						break
					}
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "STOP")); err == nil {
				if logins.Load() != 1 || logouts.Load() != 1 || count != 1 || !peerSent || historySuppliedInbound.Load() {
					t.Fatal("desktop login/message/logout chain incomplete")
				}
				write("SERVER-RESULT.json", map[string]any{"normal_router": true, "login_200": true,
					"logout_204": true, "outbound_message_persisted": true, "peer_message_sent": true,
					"postgres_version": pgVersion, "backend_revision": fixtureRevision, "peer_history_fallback_absent": true,
					"desktop_hub_connection_observed": true, "s3_persistence_qualified": false, "media_and_games_qualified": false})
				return
			}
		}
	}
}

func desktopFixtureSecret(t *testing.T) string {
	t.Helper()
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		t.Fatal("fixture randomness failed")
	}
	return hex.EncodeToString(bytes)
}

func desktopFixtureCertificate(t *testing.T) (tls.Certificate, []byte) {
	t.Helper()
	rootKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("fixture CA key failed")
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Mnema disposable GUI fixture"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal("fixture CA failed")
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("fixture server key failed")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: root.NotBefore, NotAfter: root.NotAfter, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, root, &leafKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal("fixture server certificate failed")
	}
	return tls.Certificate{Certificate: [][]byte{leafDER, rootDER}, PrivateKey: leafKey}, rootDER
}
