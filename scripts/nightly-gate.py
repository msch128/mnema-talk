#!/usr/bin/env python3
"""Decides whether the nightly sweeps let release.yml cut a release.

Usage: nightly-gate.py --list
       nightly-gate.py [--now ISO-8601] [--override REASON] [--summary FILE]
                       [--note-file FILE] <dir> [<workflow file>...]

--list prints the gated workflows (GATED), one per line. Without workflow
files the gate judges GATED. For every workflow file <dir> holds the answers
of the GitHub API:
  <file>.workflow.json         GET /repos/{repo}/actions/workflows/{file}
  <file>.runs.json             GET .../actions/workflows/{file}/runs?per_page=100
                               (every event and branch: the run numbers
                               must have no gap, see below)
  <file>.attempt1-<id>.json    GET .../actions/runs/{id}/attempts/1, for a
                               scheduled run on main with run_attempt > 1

The most recent completed scheduled run on main decides:
  conclusion success, at most 48 hours old  -> pass
  any other conclusion (failure, timed_out,
  cancelled, startup_failure, ...)          -> block (red night)
  older than 48 hours                       -> block (stale: the nightly
                                               stopped running and must not
                                               look green)
  re-run (run_attempt > 1)                  -> shown as a re-run; passes only
                                               when its first attempt also
                                               succeeded, blocks when that
                                               failed or cannot be read
  a run number missing between it and the
  newest run of the workflow                -> block (a deleted run)
A run that is still in progress is no verdict yet, so it neither passes nor
blocks: the completed run before it decides. A workflow without any completed
scheduled run passes only while it is younger than 48 hours (its first night
has not come yet); after that it blocks like a stale one. A workflow that is
not active (disabled by hand or for inactivity) blocks. A deleted run is seen
only once a later run of the workflow exists (the next night at the latest).

--override REASON (non-blank) lets a blocked gate pass; it is meant only for
the documented manual override of release.yml (doc/development.md, Releases)
and is printed with the actor (GITHUB_ACTOR) as a warning and in the summary.
--note-file FILE: with an override, the line for the release notes is
written to FILE (release.yml appends it to the release it cuts).

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
import re
import sys

MAX_AGE = timedelta(hours=48)

# The deep nightly sweeps: a red, stale or missing night blocks a release.
GATED = ('sim-sweep.yml', 'visibility-sweep.yml', 'store-trace.yml', 'plain-sweep.yml', 'clip-sweep.yml')

# Every other scheduled workflow, with the reason it does not gate a release
# (scripts/tests/test_nightly_gate.py fails on a scheduled workflow in neither).
NOT_GATED = {
    'release-check.yml': 'checks what the last release published; a red run means the release '
                         'pipeline drifted, and blocking the next release would block its repair',
    'image-scan.yml': 'weekly scan of the image already shipped for new vulnerabilities; '
                      'blocking would block the release that fixes them',
    'codeql.yml': 'weekly; CodeQL also runs on every pull request as a required check',
    'trust-expiry.yml': 'weekly operational alert that the desktop trust statement expires soon; '
                        'not a property of the code being released',
}


def parse_time(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00')).astimezone(timezone.utc)


def scheduled_main_runs(runs):
    picked = [r for r in runs if r.get('event') == 'schedule' and r.get('head_branch') == 'main']
    return sorted(picked, key=lambda r: (parse_time(r['created_at']), r['id']), reverse=True)


def missing_numbers(runs, since):
    """Run numbers from `since` up to the newest run of the workflow that no run carries."""
    numbers = {r['run_number'] for r in runs}
    return sorted(set(range(since, max(numbers) + 1)) - numbers)


def judge_attempt(name, last, first_attempt):
    """Returns (verdict, note) for a re-run deciding run, or None for a first attempt."""
    attempt = last.get('run_attempt') or 1
    if attempt <= 1:
        return None
    if first_attempt is None:
        return 'block', (f'{name}: the last scheduled run is a re-run (attempt {attempt}) and its '
                         'first attempt could not be read')
    if first_attempt.get('conclusion') != 'success':
        return 'block', (f'{name}: the last scheduled run is a re-run (attempt {attempt}); its first '
                         f'attempt concluded {first_attempt.get("conclusion")}')
    return 'pass', f' (a re-run, attempt {attempt}; the first attempt succeeded too)'


def judge(name, workflow, runs, now, first_attempts=None):
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
    gap = missing_numbers(runs, last['run_number'])
    if gap:
        return 'block', (f'{name}: run numbers {", ".join(map(str, gap))} are missing after the last '
                         f'completed scheduled run (a deleted run): {where}')
    if age > MAX_AGE:
        return 'block', f'{name}: the last completed scheduled run is {hours(age)} old (limit 48 h): {where}'
    if last.get('conclusion') != 'success':
        return 'block', f'{name}: the last scheduled run concluded {last.get("conclusion")}: {where}'
    rerun = judge_attempt(name, last, (first_attempts or {}).get(last['id']))
    if rerun and rerun[0] == 'block':
        return 'block', f'{rerun[1]}: {where}'
    rerun_note = rerun[1] if rerun else ''
    return 'pass', f'{name}: the last scheduled run succeeded {hours(age)} ago{rerun_note}{running}: {where}'


def hours(delta):
    return f'{delta.total_seconds() / 3600:.0f} h'


def blocked(results):
    return any(verdict == 'block' for _, verdict, _ in results)


def report(results, override, actor):
    lines = []
    for name, verdict, message in results:
        kind = 'error' if verdict == 'block' and not override else 'notice'
        lines.append(f'::{kind}::nightly gate: {message}')
    if override:
        lines.append(f'::warning::nightly gate overridden by {actor}: {override}')
    elif blocked(results):
        lines.append('::error::a nightly sweep is red, re-run, stale or missing; not releasing. '
                     'Only first attempts of scheduled runs count: fix the cause and let the next '
                     'night pass, or, for an urgent security fix only, use the documented override '
                     '(doc/development.md, Releases).')
    return lines


def summary(results, override, actor):
    rows = ['### Nightly sweeps', '', '| Workflow | Verdict | Detail |', '| --- | --- | --- |']
    rows += [f'| {name} | {verdict} | {message} |' for name, verdict, message in results]
    if override:
        rows += ['', f'**Overridden** by {actor}. Reason: {override}']
    return '\n'.join(rows) + '\n'


def release_note(override, actor, was_blocked):
    """One plain line for the release notes: the actor in code marks and no '@' at all, so
    GitHub renders no user link and notifies nobody; no HTML or backticks from the reason."""
    reason = re.sub(r'[<>`]', '', override).replace('@', '(at)')
    login = re.sub(r'[^A-Za-z0-9-]', '', actor) or 'unknown'
    what = 'despite a red, re-run, stale or missing nightly sweep' if was_blocked \
        else 'with the nightly override given, though every nightly sweep was green'
    return f'Released by a manual run of `{login}` {what}. Reason given: {reason}'


def first_attempts_in(directory, name):
    found = {}
    for path in Path(directory).glob(f'{name}.attempt1-*.json'):
        run_id = int(path.name.rsplit('-', 1)[1].removesuffix('.json'))
        found[run_id] = json.loads(path.read_text())
    return found


def read_results(args, now):
    results = []
    for name in args.workflows or GATED:
        workflow = json.loads((Path(args.dir) / f'{name}.workflow.json').read_text())
        runs = json.loads((Path(args.dir) / f'{name}.runs.json').read_text())['workflow_runs']
        verdict, message = judge(name, workflow, runs, now, first_attempts_in(args.dir, name))
        results.append((name, verdict, message))
    return results


def main(argv):
    if argv[1:] == ['--list']:
        print('\n'.join(GATED))
        return 0
    parser = argparse.ArgumentParser()
    parser.add_argument('--now')
    parser.add_argument('--override', default='')
    parser.add_argument('--summary')
    parser.add_argument('--note-file')
    parser.add_argument('dir')
    parser.add_argument('workflows', nargs='*')
    args = parser.parse_args(argv[1:])
    override = ' '.join(args.override.split())
    actor = os.environ.get('GITHUB_ACTOR') or 'unknown'
    try:
        now = parse_time(args.now) if args.now else datetime.now(timezone.utc)
        results = read_results(args, now)
    except (OSError, ValueError, KeyError, TypeError) as err:
        print(f'::error::nightly gate: cannot read the answers: {err}')
        return 2
    for line in report(results, override, actor):
        print(line)
    if args.summary:
        with open(args.summary, 'a') as f:
            f.write(summary(results, override, actor))
    if override and args.note_file:
        Path(args.note_file).write_text(release_note(override, actor, blocked(results)) + '\n')
    return 1 if blocked(results) and not override else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv))
