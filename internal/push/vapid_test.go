package push

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/ssrf"
)

// script is a database that answers each QueryRow and Exec in order.
type script struct {
	rows  []pgx.Row
	execs []error
}

type answer struct {
	keys Keys
	err  error
}

func (a answer) Scan(dest ...any) error {
	if a.err != nil {
		return a.err
	}
	*dest[0].(*string), *dest[1].(*string) = a.keys.Public, a.keys.Private
	return nil
}

func (s *script) QueryRow(context.Context, string, ...any) pgx.Row {
	next := s.rows[0]
	s.rows = s.rows[1:]
	return next
}

func (s *script) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	next := s.execs[0]
	s.execs = s.execs[1:]
	return pgconn.CommandTag{}, next
}

func (s *script) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not scripted")
}

func TestKeysLoadingReportsEveryFailureInsteadOfInventingNewKeys(t *testing.T) {
	good := testKeys(t)
	down := errors.New("connection refused")
	for name, c := range map[string]struct {
		db   *script
		want string
	}{
		"reading fails":            {&script{rows: []pgx.Row{answer{err: down}}}, "read vapid keys"},
		"storing fails":            {&script{rows: []pgx.Row{answer{err: pgx.ErrNoRows}}, execs: []error{down}}, "store vapid keys"},
		"reading back fails":       {&script{rows: []pgx.Row{answer{err: pgx.ErrNoRows}, answer{err: down}}, execs: []error{nil}}, "read vapid keys"},
		"a stored pair is garbled": {&script{rows: []pgx.Row{answer{keys: Keys{Public: "x", Private: "y"}}}}, "malformed"},
	} {
		if _, err := LoadKeys(context.Background(), c.db); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error about %q", name, err, c.want)
		}
	}
	keys, err := LoadKeys(context.Background(), &script{rows: []pgx.Row{answer{keys: good}}})
	if err != nil || keys != good {
		t.Fatalf("a stored pair: %v %v", keys, err)
	}
	// A server that loses the race to create the pair uses the winner's.
	keys, err = LoadKeys(context.Background(), &script{rows: []pgx.Row{answer{err: pgx.ErrNoRows}, answer{keys: good}}, execs: []error{nil}})
	if err != nil || keys != good {
		t.Fatalf("after creating: %v %v", keys, err)
	}
}

func TestDeliveryFailuresAreNamedByACodeNotByTheNetworkError(t *testing.T) {
	timeout := &net.DNSError{IsTimeout: true}
	for err, want := range map[error]string{
		fmt.Errorf("post: %w", ssrf.ErrBlocked):          "blocked_address",
		fmt.Errorf("post: %w", ssrf.ErrResolve):          "dns_error",
		fmt.Errorf("post: %w", ssrf.ErrConnect):          "connect_error",
		fmt.Errorf("post: %w", ssrf.ErrBadURL):           "invalid_url",
		fmt.Errorf("post: %w", context.DeadlineExceeded): "timeout",
		timeout:                               "timeout",
		errors.New("tls: handshake failure"):  "tls_error",
		errors.New("x509: unknown authority"): "tls_error",
		errors.New("something else entirely"): "request_error",
	} {
		if got := classifyError(err); got != want {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
}

func TestSenderNameFallsBackToTheUsername(t *testing.T) {
	if got := sender(chat.Message{Username: "alice"}); got != "alice" {
		t.Fatalf("%q", got)
	}
	if got := sender(chat.Message{Username: "alice", DisplayName: "Alice"}); got != "Alice" {
		t.Fatalf("%q", got)
	}
}
