//go:build integration

package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
)

func nativeContentFixture(t *testing.T) (chatFixture, *auth.NativeSessions, *NativeCiphertextStore, auth.NativePrincipal) {
	t.Helper()
	f := newChatFixture(t)
	sessions, store, principal := nativeContentLogin(t, f)
	return f, sessions, store, principal
}

func nativeContentLogin(t *testing.T, f chatFixture) (*auth.NativeSessions, *NativeCiphertextStore, auth.NativePrincipal) {
	t.Helper()
	const password = "fixture-native-content-password"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(context.Background(), `UPDATE users SET password_hash=$1 WHERE id=$2`, hash, f.u.ID); err != nil {
		t.Fatal(err)
	}
	sessions, err := auth.NewNativeSessions(f.p, auth.NativePolicy{SessionLifetime: 24 * time.Hour, FamilyLifetime: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	accounts := auth.NewHandler(&auth.Sessions{DB: f.p, Secret: strings.Repeat("s", 32), TTL: 24 * time.Hour, Secure: true}, nil)
	handler, err := auth.NewNativeHandler(accounts, sessions)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"username": f.u.Username, "password": password, "client_instance_id": uuid.NewString()})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("native fixture login status %d", recorder.Code)
	}
	var grant struct {
		Access string `json:"access_token"`
	}
	if json.Unmarshal(recorder.Body.Bytes(), &grant) != nil || len(grant.Access) != 43 {
		t.Fatal("invalid fixture grant")
	}
	principal, err := sessions.AuthenticateAccess(context.Background(), grant.Access)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewNativeCiphertextStore(sessions)
	if err != nil {
		t.Fatal(err)
	}
	return sessions, store, principal
}

func TestNativeCiphertextConcurrentExactRetry(t *testing.T) {
	f, _, store, principal := nativeContentFixture(t)
	request := OpaqueCiphertextRequest{f.c.ID, uuid.New(), []byte("fixture-group"), []byte{0, 255, 128, 7}}
	const n = 8
	var wg sync.WaitGroup
	results := make(chan NativeCiphertextRecord, n)
	created := make(chan bool, n)
	failures := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, c, e := store.Publish(context.Background(), principal, request)
			results <- r
			created <- c
			failures <- e
		}()
	}
	wg.Wait()
	close(results)
	close(created)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	count := 0
	for c := range created {
		if c {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("created %d events", count)
	}
	var id uuid.UUID
	for r := range results {
		w := r.Wire()
		if id == uuid.Nil {
			id = w.ID
		}
		if w.ID != id || !bytes.Equal(w.Ciphertext, request.Ciphertext) || w.UserID != f.u.ID {
			t.Fatal("retry changed persisted identity/bytes")
		}
		w.Ciphertext[0] = 42
		if r.Wire().Ciphertext[0] != 0 {
			t.Fatal("wire alias")
		}
		if strings.Contains(fmt.Sprintf("%+v", r), "fixture-group") {
			t.Fatal("record leaked in log")
		}
	}
	changed := request
	changed.Ciphertext = []byte{1}
	if _, _, e := store.Publish(context.Background(), principal, changed); !errors.Is(e, ErrNativeContentConflict) {
		t.Fatalf("ciphertext conflict: %v", e)
	}
	changed = request
	changed.GroupID = []byte("other")
	if _, _, e := store.Publish(context.Background(), principal, changed); !errors.Is(e, ErrNativeContentConflict) {
		t.Fatalf("group conflict: %v", e)
	}
	var rows int
	if e := f.p.QueryRow(context.Background(), `SELECT count(*) FROM native_ciphertext_events`).Scan(&rows); e != nil || rows != 1 {
		t.Fatalf("rows=%d err=%v", rows, e)
	}
}

func TestNativeCiphertextPaginationBoundsAndRevocation(t *testing.T) {
	f, sessions, store, principal := nativeContentFixture(t)
	for i := 0; i < 3; i++ {
		if _, created, e := store.Publish(context.Background(), principal, OpaqueCiphertextRequest{f.c.ID, uuid.New(), []byte{1}, []byte{byte(i)}}); e != nil || !created {
			t.Fatalf("publish: %v", e)
		}
	}
	first, e := store.List(context.Background(), principal, f.c.ID, 0, 2)
	if e != nil || len(first) != 2 {
		t.Fatalf("first page: %v count%d", e, len(first))
	}
	last, e := store.List(context.Background(), principal, f.c.ID, first[1].Wire().Number, 2)
	if e != nil || len(last) != 1 || last[0].Wire().Ciphertext[0] != 2 {
		t.Fatal("pagination dropped/repeated event")
	}
	if _, e = store.List(context.Background(), principal, uuid.New(), 0, 2); !errors.Is(e, ErrNativeContentChannel) {
		t.Fatalf("unknown channel: %v", e)
	}
	if _, e = store.List(context.Background(), principal, f.c.ID, 0, 101); !errors.Is(e, ErrNativeContentInput) {
		t.Fatal("unbounded list")
	}
	if _, _, e = store.Publish(context.Background(), principal, OpaqueCiphertextRequest{f.c.ID, uuid.New(), []byte{1}, make([]byte, 65537)}); !errors.Is(e, ErrNativeContentInput) {
		t.Fatal("unbounded ciphertext")
	}
	if e = sessions.RevokeFamily(context.Background(), principal); e != nil {
		t.Fatal(e)
	}
	if _, e = store.List(context.Background(), principal, f.c.ID, 0, 2); !errors.Is(e, auth.ErrNativeUnauthorized) {
		t.Fatalf("revoked read: %v", e)
	}
	if _, _, e = store.Publish(context.Background(), principal, OpaqueCiphertextRequest{f.c.ID, uuid.New(), []byte{1}, []byte{1}}); !errors.Is(e, auth.ErrNativeUnauthorized) {
		t.Fatalf("revoked write: %v", e)
	}
}

func TestNativeCiphertextCursorWaitsForEarlierCommit(t *testing.T) {
	f, sessions, store, principal := nativeContentFixture(t)
	other := &auth.User{Username: "second_fixture_member", DisplayName: "Second", Role: auth.RoleUser}
	if e := f.p.QueryRow(context.Background(), `INSERT INTO users(username,display_name,password_hash) VALUES($1,$2,'unused') RETURNING id`, other.Username, other.DisplayName).Scan(&other.ID); e != nil {
		t.Fatal(e)
	}
	_, _, reader := nativeContentLogin(t, chatFixture{p: f.p, u: other, c: f.c})
	entered := make(chan struct{})
	release := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- sessions.WithAuthorizedTransaction(context.Background(), principal, func(ctx context.Context, tx pgx.Tx, _ auth.User) error {
			var channel uuid.UUID
			if e := tx.QueryRow(ctx, `SELECT id FROM channels WHERE id=$1 FOR UPDATE`, f.c.ID).Scan(&channel); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, `INSERT INTO native_ciphertext_events(id,channel_id,user_id,client_event_id,group_id,ciphertext) VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), f.c.ID, f.u.ID, uuid.New(), []byte{1}, []byte{1}); e != nil {
				return e
			}
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case e := <-writerDone:
		t.Fatalf("writer failed before barrier: %v", e)
	case <-time.After(5 * time.Second):
		t.Fatal("writer barrier timeout")
	}
	published := make(chan error, 1)
	listed := make(chan []NativeCiphertextRecord, 1)
	listErrors := make(chan error, 1)
	go func() {
		_, _, e := store.Publish(context.Background(), reader, OpaqueCiphertextRequest{f.c.ID, uuid.New(), []byte{1}, []byte{2}})
		published <- e
	}()
	go func() { r, e := store.List(context.Background(), reader, f.c.ID, 0, 100); listed <- r; listErrors <- e }()
	select {
	case e := <-published:
		close(release)
		t.Fatalf("writer advanced past uncommitted event: %v", e)
	case <-time.After(75 * time.Millisecond):
	}
	close(release)
	if e := <-writerDone; e != nil {
		t.Fatal(e)
	}
	if e := <-published; e != nil {
		t.Fatal(e)
	}
	page := <-listed
	if e := <-listErrors; e != nil {
		t.Fatal(e)
	}
	// Depending on the reader/account lock winner, it sees the first commit or both;
	// it can never return the second commit while skipping the first.
	if len(page) < 1 || page[0].Wire().Ciphertext[0] != 1 {
		t.Fatal("cursor skipped pending earlier commit")
	}
	all, e := store.List(context.Background(), reader, f.c.ID, 0, 100)
	if e != nil || len(all) != 2 || all[0].Wire().Number >= all[1].Wire().Number {
		t.Fatal("commit order does not match cursor")
	}
}
