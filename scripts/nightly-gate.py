#!/usr/bin/env python3
"""Decides whether the nightly sweeps let release.yml cut a release.

Usage: nightly-gate.py [--now ISO-8601] [--override REASON] [--summary FILE]
                       <dir> <workflow file>...

For every workflow file (for example sim-sweep.yml) <dir> holds two answers
of the GitHub API:
  <file>.workflow.json  GET /repos/{repo}/actions/workflows/{file}
  <file>.runs.json      GET .../actions/workflows/{file}/runs?branch=main&event=schedule

The most recent completed scheduled run on main decides:
  conclusion success, at most 48 hours old  -> pass
  any other conclusion (failure, timed_out,
  cancelled, startup_failure, ...)          -> block (red night)
  older than 48 hours                       -> block (stale: the nightly
                                               stopped running and must not
                                               look green)
A run that is still in progress is no verdict yet, so it neither passes nor
blocks: the completed run before it decides. A workflow without any completed
scheduled run passes only while it is younger than 48 hours (its first night
has not come yet); after that it blocks like a stale one. A workflow that is
not active (disabled by hand or for inactivity) blocks.

--override REASON (non-blank) lets a blocked gate pass; it is meant only for
the documented manual override of release.yml (doc/development.md, Releases)
and is printed with the actor (GITHUB_ACTOR) as a warning and in the summary.

stdout: GitHub workflow commands (::error:: per blocking workflow, ::warning::
for an override, ::notice:: otherwise). --summary FILE: a Markdown table is
appended to FILE (the job summary).
Exit status: 0 pass, 1 blocked, 2 bad input.
"""
import argparse
from datetime import datetime, timedelta, timezone
import json
import os
from pathlib import Path
import sys

MAX_AGE = timedelta(hours=48)


def parse_time(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00')).astimezone(timezone.utc)


def scheduled_main_runs(runs):
    """The API is asked for these already; filter again so a wrong query cannot widen the gate."""
    picked = [r for r in runs if r.get('event') == 'schedule' and r.get('head_branch') == 'main']
    return sorted(picked, key=lambda r: (parse_time(r['created_at']), r['id']), reverse=True)


def judge(name, workflow, runs, now):
    """Returns (verdict, message) with verdict 'pass' or 'block'."""
    if workflow.get('state') != 'active':
        return 'block', f'{name} is {workflow.get("state")}, so its nights do not run'
    ordered = scheduled_main_runs(runs)
    completed = [r for r in ordered if r.get('status') == 'completed']
    running = ' (a newer run is still in progress)' if ordered and ordered[0].get('status') != 'completed' else ''
    if not completed:
        age = now - parse_time(workflow['created_at'])
        if age <= MAX_AGE:
            return 'pass', f'{name} has no completed scheduled run yet; it was added {hours(age)} ago{running}'
        return 'block', f'{name} has no completed scheduled run on main although it was added {hours(age)} ago'
    last = completed[0]
    age = now - parse_time(last['created_at'])
    where = f'{last.get("html_url", last["id"])}'
    if age > MAX_AGE:
        return 'block', f'{name}: the last completed scheduled run is {hours(age)} old (limit 48 h): {where}'
    if last.get('conclusion') != 'success':
        return 'block', f'{name}: the last scheduled run concluded {last.get("conclusion")}: {where}'
    return 'pass', f'{name}: the last scheduled run succeeded {hours(age)} ago{running}: {where}'


def hours(delta):
    return f'{delta.total_seconds() / 3600:.0f} h'


def decide(results, override):
    """results: [(name, verdict, message)]. Returns the exit status."""
    blocked = any(verdict == 'block' for _, verdict, _ in results)
    if blocked and not override:
        return 1
    return 0


def report(results, override, actor):
    lines = []
    for name, verdict, message in results:
        kind = 'error' if verdict == 'block' and not override else 'notice'
        lines.append(f'::{kind}::nightly gate: {message}')
    if override:
        lines.append(f'::warning::nightly gate overridden by {actor}: {override}')
    elif any(verdict == 'block' for _, verdict, _ in results):
        lines.append('::error::a nightly sweep is red, stale or missing; not releasing. '
                     'Only scheduled runs count: let the next night pass (or re-run the failed '
                     'scheduled run when it failed for a reason outside the code), or, for an urgent security fix only, '
                     'use the documented override (doc/development.md, Releases).')
    return lines


def summary(results, override, actor):
    rows = ['### Nightly sweeps', '', '| Workflow | Verdict | Detail |', '| --- | --- | --- |']
    rows += [f'| {name} | {verdict} | {message} |' for name, verdict, message in results]
    if override:
        rows += ['', f'**Overridden** by {actor}. Reason: {override}']
    return '\n'.join(rows) + '\n'


def main(argv):
    parser = argparse.ArgumentParser()
    parser.add_argument('--now')
    parser.add_argument('--override', default='')
    parser.add_argument('--summary')
    parser.add_argument('dir')
    parser.add_argument('workflows', nargs='+')
    args = parser.parse_args(argv[1:])
    now = parse_time(args.now) if args.now else datetime.now(timezone.utc)
    override = ' '.join(args.override.split())
    actor = os.environ.get('GITHUB_ACTOR') or 'unknown'
    results = []
    for name in args.workflows:
        try:
            workflow = json.loads((Path(args.dir) / f'{name}.workflow.json').read_text())
            runs = json.loads((Path(args.dir) / f'{name}.runs.json').read_text())['workflow_runs']
            verdict, message = judge(name, workflow, runs, now)
        except (OSError, ValueError, KeyError, TypeError) as err:
            print(f'::error::nightly gate: cannot read the answers for {name}: {err}')
            return 2
        results.append((name, verdict, message))
    for line in report(results, override, actor):
        print(line)
    if args.summary:
        with open(args.summary, 'a') as f:
            f.write(summary(results, override, actor))
    return decide(results, override)


if __name__ == '__main__':
    sys.exit(main(sys.argv))
