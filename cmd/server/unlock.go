package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
)

const unlockUsage = "usage: mnema-talk unlock <username> | mnema-talk unlock --all"

// unlockWait is how long the command waits for the running server to apply the
// request before it says the request stays queued.
var unlockWait = 15 * time.Second

func validUnlockArgs(all bool, narg int) bool {
	if all {
		return narg == 0
	}
	return narg == 1
}

// unlockCommand clears login lockouts of the running server. The server holds
// them in memory, so this process queues a request in the database (the
// server polls it) and then waits for the server to pick it up.
func unlockCommand(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("unlock", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	all := fs.Bool("all", false, "clear the lockouts of every account")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return fmt.Errorf("%w\n%s", err, unlockUsage)
	}
	if !validUnlockArgs(*all, fs.NArg()) {
		return errors.New(unlockUsage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return requestUnlock(ctx, pool, fs.Arg(0), *all, out)
}

// reorderFlags lets the flag come after the username.
func reorderFlags(args []string) []string {
	var flags, rest []string
	for _, a := range args {
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(flags, rest...)
}

func printUnlockSuccess(out io.Writer, username string, all bool) {
	if all {
		fmt.Fprintln(out, "Login lockouts cleared for all accounts.")
		return
	}
	fmt.Fprintf(out, "Login lockouts cleared for %s.\n", username)
}

func waitForUnlock(ctx context.Context, q db.Querier, id int64, username string, all bool, out io.Writer) error {
	deadline := time.Now().Add(unlockWait)
	for {
		pending, err := auth.UnlockRequestPending(ctx, q, id)
		if err != nil {
			return err
		}
		if !pending {
			printUnlockSuccess(out, username, all)
			return nil
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(out, "Request queued; the server applies it as soon as it is running (it was not seen within the wait time).")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func requestUnlock(ctx context.Context, q db.Querier, username string, all bool, out io.Writer) error {
	id, err := auth.RequestUnlock(ctx, q, username, all)
	if errors.Is(err, auth.ErrUnlockUnknownUser) {
		return fmt.Errorf("no user named %q", username)
	}
	if err != nil {
		return err
	}
	return waitForUnlock(ctx, q, id, username, all, out)
}
