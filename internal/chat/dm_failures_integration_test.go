//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestDirectMessageOperationsReportDatabaseFailures(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var bob uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('bob_dm', 'Bob', 'x') RETURNING id`).Scan(&bob); err != nil {
		t.Fatal(err)
	}
	id, created, err := CreateDM(ctx, f.p, NewDM{Creator: f.u.ID, Others: []uuid.UUID{bob}})
	if err != nil || !created {
		t.Fatalf("setup: %v %v", created, err)
	}
	// A group needs members that exist and are eligible.
	if _, _, err := CreateDM(ctx, f.p, NewDM{Creator: f.u.ID, Others: []uuid.UUID{bob, uuid.New()}}); err != errDMStart {
		t.Fatalf("group with an unknown member: %v", err)
	}
	var carol uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('carol_dm', 'Carol', 'x') RETURNING id`).Scan(&carol); err != nil {
		t.Fatal(err)
	}
	g, _, err := CreateDM(ctx, f.p, NewDM{Creator: f.u.ID, Others: []uuid.UUID{bob, carol}})
	if err != nil {
		t.Fatal(err)
	}

	broken := map[string]string{"dm_members": `SELECT NULL::uuid AS channel_id`}
	cases := map[string]func() error{
		"list": func() error { _, err := ListDMs(ctx, driftPool(t, f.p, broken), f.u.ID, nil); return err },
		"members": func() error {
			_, err := ListDMs(ctx, driftPool(t, f.p, map[string]string{"users": `SELECT NULL::uuid AS id`}), f.u.ID, nil)
			return err
		},
		"create": func() error {
			_, _, err := CreateDM(ctx, driftPool(t, f.p, broken), NewDM{Creator: f.u.ID, Others: []uuid.UUID{carol}})
			return err
		},
		"add":        func() error { _, err := AddDMMember(ctx, driftPool(t, f.p, broken), g, f.u.ID, uuid.New()); return err },
		"leave":      func() error { _, _, err := LeaveDM(ctx, driftPool(t, f.p, broken), g, f.u.ID); return err },
		"member ids": func() error { _, err := DMMemberIDs(ctx, driftPool(t, f.p, broken), id); return err },
		"get policy": func() error {
			_, err := GetDMPolicy(ctx, driftPool(t, f.p, map[string]string{"users": `SELECT NULL::uuid AS id`}), f.u.ID)
			return err
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("expected a database error")
			}
		})
	}
}
