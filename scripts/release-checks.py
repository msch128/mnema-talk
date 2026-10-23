#!/usr/bin/env python3
"""Decides which commit statuses release.yml mirrors onto a release PR.

Usage: release-checks.py <rules.json> <check-runs.jsonl>

rules.json is the answer of GET /repos/{repo}/rules/branches/main (a JSON
array of rules); the contexts that main requires are read from its
required_status_checks rules. check-runs.jsonl holds one check run object per
line (the runs of the commit CI just tested).

For every required context the newest run with that name decides:
  success, neutral, skipped  -> mirrored as success
  completed otherwise        -> failure
  none, or not completed     -> pending

stdout: one "<context>\t<success|failure|pending>" line per required context.
Exit status: 0 when every required context succeeded, 1 when one failed (or no
context is required at all, which would turn the gate into a no-op), 3 when
none failed but some are still pending.
"""
import json
import sys

PASSING = {'success', 'neutral', 'skipped'}


def required_contexts(rules):
    seen = []
    for rule in rules:
        if rule.get('type') != 'required_status_checks':
            continue
        for check in rule.get('parameters', {}).get('required_status_checks', []):
            if check['context'] not in seen:
                seen.append(check['context'])
    return seen


def newest_runs(runs):
    newest = {}
    for run in runs:
        name = run['name']
        if name not in newest or run['id'] > newest[name]['id']:
            newest[name] = run
    return newest


def state_of(run):
    if run is None or run.get('status') != 'completed':
        return 'pending'
    return 'success' if run.get('conclusion') in PASSING else 'failure'


def decide(rules, runs):
    contexts = required_contexts(rules)
    newest = newest_runs(runs)
    return [(context, state_of(newest.get(context))) for context in contexts]


def exit_code(decisions):
    if not decisions:
        return 1
    states = {state for _, state in decisions}
    if 'failure' in states:
        return 1
    return 3 if 'pending' in states else 0


def main(argv):
    with open(argv[1]) as f:
        rules = json.load(f)
    with open(argv[2]) as f:
        runs = [json.loads(line) for line in f if line.strip()]
    decisions = decide(rules, runs)
    for context, state in decisions:
        print(f'{context}\t{state}')
    return exit_code(decisions)


if __name__ == '__main__':
    sys.exit(main(sys.argv))
