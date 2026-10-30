package setupwizard

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

// Exec runs a command in dir with its output on out; tests replace it.
type Exec func(ctx context.Context, dir string, out io.Writer, name string, args ...string) error

func systemExec(ctx context.Context, dir string, out io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, out, out
	return cmd.Run()
}

// canStart is whether this process can start the stack itself: it runs on the
// machine (not in the docker run --rm container) and Docker Compose works.
func canStart(ctx context.Context, p Probe) bool {
	if p.Host != nil && p.Host().InContainer {
		return false
	}
	return p.Compose != nil && p.Compose(ctx) == nil
}

// offerStart starts the stack and runs the live checks, when asked (--start,
// or yes to the question) and possible. It never fails the setup: the files are
// written, and the output says what to do.
func offerStart(ctx context.Context, p *prompter, o Options, dir string, env Env, interactive bool) {
	if !canStart(ctx, env.Probe) {
		if o.Start {
			fmt.Fprintln(p.out, "\n--start needs Docker Compose on this machine, outside a container; run the commands under \"What to do next\" yourself.")
		}
		return
	}
	if !o.Start {
		if !interactive {
			return
		}
		p.explain("Start it now? (optional)",
			"Runs "+startCommand+" in "+dir+", then the same setup checks as the",
			"admin console's Status tab. The first start downloads the images (a few minutes).")
		yes, err := p.askYes("Start the stack and check it now?")
		if err != nil || !yes {
			return
		}
	}
	runStart(ctx, p.out, dir, env.exec())
}

func runStart(ctx context.Context, out io.Writer, dir string, run Exec) {
	fmt.Fprintln(out, "\n==> "+startCommand)
	if err := run(ctx, dir, out, "docker", "compose", "up", "-d", "--wait", "--wait-timeout", "300"); err != nil {
		fmt.Fprintf(out, "\nThe stack did not become healthy (%v). See why with: docker compose logs app\n", err)
		return
	}
	fmt.Fprintln(out, "\n==> docker compose exec app mnema-talk doctor")
	if err := run(ctx, dir, out, "docker", "compose", "exec", "-T", "app", "mnema-talk", "doctor"); err != nil {
		fmt.Fprintln(out, "\nSome checks need attention: the fix lines above say what to change. Items about your router, such as forwarding the ports, cannot be seen from here.")
	}
}

func (e Env) exec() Exec {
	if e.Exec != nil {
		return e.Exec
	}
	return systemExec
}
