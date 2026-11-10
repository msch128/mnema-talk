package perm

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A component wired without a permission service must not answer "allowed":
// every question fails, for the Owner too.
func TestANilServiceAnswersNoPermissionQuestion(t *testing.T) {
	var none *Service
	ctx, owner, ch := context.Background(), Subject{ID: uuid.New(), Owner: true}, uuid.New()
	if p, err := none.Global(ctx, owner); err == nil || p != 0 {
		t.Errorf("Global = %v, %v", p, err)
	}
	if p, err := none.Channel(ctx, owner, ch); err == nil || p != 0 {
		t.Errorf("Channel = %v, %v", p, err)
	}
	for name, err := range map[string]error{
		"RequireGlobal":  none.RequireGlobal(ctx, owner, ViewChannel),
		"RequireChannel": none.RequireChannel(ctx, owner, ch, ViewChannel),
		"RequireCall":    none.RequireCall(ctx, owner),
		"Locked":         none.Locked(ctx, owner, func(pgx.Tx, *Snapshot) error { return nil }),
	} {
		if err == nil {
			t.Errorf("%s let the caller through", name)
		}
	}
	if snap, err := none.Snapshot(ctx); err == nil || snap != nil {
		t.Errorf("Snapshot = %v, %v", snap, err)
	}
	if snap, err := none.ForChannel(ctx, ch); err == nil || snap != nil {
		t.Errorf("ForChannel = %v, %v", snap, err)
	}
}

func TestMustStopsAComponentBuiltWithoutAService(t *testing.T) {
	svc := NewServiceFrom(func(context.Context, time.Time) (*Snapshot, error) { return nil, nil })
	if Must(svc, "test") != svc {
		t.Fatal("Must changed the service")
	}
	defer func() {
		if r := recover(); r != "test: needs a permission service" {
			t.Fatalf("recovered %v", r)
		}
	}()
	Must(nil, "test")
}
