//go:build integration

package chattest

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// NewMember resets the test database and inserts one ordinary member.
func NewMember(t *testing.T) (*db.Pool, *auth.User) {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	u := &auth.User{Username: "coverage_member", DisplayName: "Coverage Member", Role: auth.RoleUser}
	if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1,$2,'test-only-unused') RETURNING id, created_at`, u.Username, u.DisplayName).Scan(&u.ID, &u.CreatedAt); err != nil {
		t.Fatal(err)
	}
	return p, u
}

// NewChannel inserts a plain text channel and returns its id. It does not go
// through chat.CreateChannel (a higher layer), so sub-packages can use it.
func NewChannel(t *testing.T, p *db.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := p.QueryRow(context.Background(), `INSERT INTO channels (name, type, topic, sort_order) VALUES ($1, 'text', '', 0) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// NewMessage inserts a top-level message of user in channel and returns its id.
func NewMessage(t *testing.T, p *db.Pool, channel, user uuid.UUID, content string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := p.QueryRow(context.Background(), `
		INSERT INTO messages (number, channel_id, user_id, content, created_at) OVERRIDING SYSTEM VALUE
		VALUES (nextval(pg_get_serial_sequence('messages', 'number')), $1, $2, $3, clock_timestamp())
		RETURNING id`, channel, user, content).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
