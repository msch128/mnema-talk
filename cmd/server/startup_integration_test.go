//go:build integration

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	auth.SetPasswordCostForTests(bcrypt.MinCost)
	os.Exit(testutil.Main(m))
}

func startupEnv(t *testing.T, pool *db.Pool) string {
	t.Helper()
	oldLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	// Real AWS client initialization talks to this local S3 protocol fixture.
	objects := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("unexpected S3 operation: %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(objects.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	_ = listener.Close()
	for key, value := range map[string]string{
		"DATABASE_URL": pool.Config().ConnString(), "APP_ENV": "development",
		"PUBLIC_URL": "http://" + address, "BIND_ADDR": "127.0.0.1", "PORT": port,
		"JWT_SECRET": "startup-test-secret-startup-test-secret", "ADMIN_USERNAME": "StartupAdmin",
		"ADMIN_INITIAL_PASSWORD": "startup-test-password", "LOG_LEVEL": "info",
		"S3_ENDPOINT": objects.URL, "S3_ACCESS_KEY": "startup-test-key", "S3_SECRET_KEY": "startup-test-value",
		"S3_BUCKET": "startup-test", "S3_REGION": "us-east-1", "S3_FORCE_PATH_STYLE": "true",
		"WEBRTC_UDP_MUX_PORT": "0", "WEBRTC_NAT_1TO1_IP": "127.0.0.1", "WEBRTC_TURN_URLS": "",
		"WEBRTC_STUN_URLS": "", "MEDIA_RETENTION_DAYS": "0", "LINK_PREVIEWS_ENABLED": "false",
		"UPDATE_CHECK_ENABLED": "false", "UPDATER_TOKEN": "", "PION_LOG_DEBUG": "",
	} {
		t.Setenv(key, value)
	}
	return address
}

func TestStartupFailureBoundaries(t *testing.T) {
	pool := testutil.DB(t)
	t.Run("database connection", func(t *testing.T) {
		startupEnv(t, pool)
		t.Setenv("DATABASE_URL", "postgres://fixture:fixture@127.0.0.1:1/fixture?sslmode=disable&connect_timeout=1")
		t.Setenv("LOG_LEVEL", "debug")
		if err := runWithContext(context.Background()); err == nil || !strings.Contains(err.Error(), "ping database") {
			t.Fatalf("database failure: %v", err)
		}
		if os.Getenv("PION_LOG_DEBUG") != "ice" {
			t.Fatal("debug logging was not enabled")
		}
	})
	t.Run("migration integrity", func(t *testing.T) {
		startupEnv(t, pool)
		ctx := context.Background()
		var filename, checksum string
		if err := pool.QueryRow(ctx, `SELECT filename, checksum FROM schema_migrations ORDER BY filename LIMIT 1`).Scan(&filename, &checksum); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET checksum='corrupted-fixture' WHERE filename=$1`, filename); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, `UPDATE schema_migrations SET checksum=$1 WHERE filename=$2`, checksum, filename); err != nil {
				t.Error(err)
			}
		})
		if err := runWithContext(ctx); err == nil || !strings.Contains(err.Error(), "changed") {
			t.Fatalf("migration integrity: %v", err)
		}
	})
	t.Run("administrator validation", func(t *testing.T) {
		testutil.Reset(t, pool)
		startupEnv(t, pool)
		t.Setenv("ADMIN_USERNAME", "!")
		if err := runWithContext(context.Background()); err == nil || !strings.Contains(err.Error(), "ADMIN_USERNAME") {
			t.Fatalf("administrator failure: %v", err)
		}
	})
	t.Run("announce validation", func(t *testing.T) {
		testutil.Reset(t, pool)
		startupEnv(t, pool)
		t.Setenv("WEBRTC_NAT_1TO1_IP", "::1")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := runWithContext(ctx); err == nil || !strings.Contains(err.Error(), "IPv4") {
			t.Fatalf("announce failure: %v", err)
		}
	})
}

func TestStartupServesAndStopsWithItsContext(t *testing.T) {
	pool := testutil.DB(t)
	for _, variant := range []string{"voice", "occupied voice port", "production warning"} {
		t.Run(variant, func(t *testing.T) {
			testutil.Reset(t, pool)
			address := startupEnv(t, pool)
			if variant == "occupied voice port" {
				udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer udp.Close()
				port := udp.LocalAddr().(*net.UDPAddr).Port
				for _, key := range []string{"WEBRTC_UDP_PORT_MIN", "WEBRTC_UDP_PORT_MAX", "WEBRTC_UDP_MUX_PORT"} {
					t.Setenv(key, strconv.Itoa(port))
				}
			}
			if variant == "production warning" {
				t.Setenv("APP_ENV", "production")
				t.Setenv("WEBRTC_NAT_1TO1_IP", "")
				t.Setenv("UPDATE_CHECK_ENABLED", "true")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runWithContext(ctx) }()
			client := &http.Client{Timeout: time.Second}
			deadline := time.Now().Add(10 * time.Second)
			for {
				resp, err := client.Get("http://" + address + "/api/health")
				if err == nil {
					var body struct{ Status string }
					err = json.NewDecoder(resp.Body).Decode(&body)
					_ = resp.Body.Close()
					if err != nil || resp.StatusCode != http.StatusOK || body.Status != "ok" {
						t.Fatalf("health status=%d err=%v body=%+v", resp.StatusCode, err, body)
					}
					break
				}
				select {
				case err := <-done:
					t.Fatalf("startup ended before serving: %v", err)
				default:
				}
				if time.Now().After(deadline) {
					t.Fatal("startup did not serve")
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("startup did not stop")
			}
			if count := func() int {
				var count int
				_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE role='admin'`).Scan(&count)
				return count
			}(); count != 1 {
				t.Fatalf("administrators=%d", count)
			}
		})
	}
}
