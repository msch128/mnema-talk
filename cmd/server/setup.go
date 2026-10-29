package main

import (
	"context"
	"io"

	"github.com/msch128/mnema-talk/internal/setupwizard"
)

// setupCommand is `mnema-talk setup`: it writes .env and docker-compose.yml
// for a new installation (see internal/setupwizard).
func setupCommand(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	return setupwizard.Run(ctx, args, setupwizard.Env{In: in, Out: out, Probe: setupwizard.DefaultProbe()})
}
