package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestStatementLimitIsOnlyEverRaised(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		base time.Duration
		ctx  context.Context
		want time.Duration
	}{
		{"unmarked context keeps the pool limit", 15 * time.Second, ctx, 15 * time.Second},
		{"a longer limit applies", 15 * time.Second, WithStatementTimeout(ctx, time.Minute), time.Minute},
		{"no limit applies", 15 * time.Second, WithStatementTimeout(ctx, 0), 0},
		{"a shorter limit is ignored", 15 * time.Second, WithStatementTimeout(ctx, time.Second), 15 * time.Second},
		{"a pool without a limit stays without one", 0, WithStatementTimeout(ctx, time.Minute), 0},
	} {
		if got := (statementLimit{base: tc.base}).wanted(tc.ctx); got != tc.want {
			t.Errorf("%s: limit %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestIsStatementTimeoutMatchesOnlyTheCancellation(t *testing.T) {
	if !IsStatementTimeout(fmt.Errorf("query: %w", &pgconn.PgError{Code: "57014"})) {
		t.Error("a wrapped statement timeout was not recognised")
	}
	if IsStatementTimeout(&pgconn.PgError{Code: "23505"}) || IsStatementTimeout(errors.New("boom")) || IsStatementTimeout(nil) {
		t.Error("another error counted as a statement timeout")
	}
}

func TestConnectRejectsAPoolSizeOutOfRange(t *testing.T) {
	for _, n := range []int{0, -1, MaxPoolConns + 1} {
		_, err := ConnectWith(context.Background(), "postgres://u:p@127.0.0.1:1/db", Options{MaxConns: n})
		if err == nil {
			t.Errorf("pool size %d accepted", n)
		}
	}
}
