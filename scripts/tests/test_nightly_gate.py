#!/usr/bin/env python3
"""scripts/nightly-gate.py: a red, re-run, stale or missing night blocks a release."""
from contextlib import redirect_stdout
import importlib.util
import io
import json
from pathlib import Path
import re
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('nightly_gate', ROOT / 'scripts' / 'nightly-gate.py')
ng = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ng)

NOW = '2026-11-14T12:00:00Z'


def workflow(created='2026-11-01T03:43:37.000+01:00', state='active'):
    return {'id': 1, 'state': state, 'created_at': created}


def run(id_, created, conclusion='success', status='completed', event='schedule', branch='main',
        number=None, attempt=1):
    return {'id': id_, 'run_number': id_ if number is None else number, 'run_attempt': attempt,
            'created_at': created, 'status': status, 'conclusion': conclusion,
            'event': event, 'head_branch': branch, 'html_url': f'https://example.invalid/runs/{id_}'}


GREEN = [run(2, '2026-11-14T02:43:00Z'), run(1, '2026-11-13T02:43:00Z', 'failure')]


def gate(answers, *extra, attempts=None):
    """Runs the script on {file: (workflow, runs)} answers and {run id: first attempt};
    returns (status, stdout, summary, note)."""
    with tempfile.TemporaryDirectory() as d:
        for name, (wf, runs) in answers.items():
            Path(d, f'{name}.workflow.json').write_text(json.dumps(wf))
            Path(d, f'{name}.runs.json').write_text(json.dumps({'total_count': len(runs), 'workflow_runs': runs}))
            for run_id, first in (attempts or {}).items():
                Path(d, f'{name}.attempt1-{run_id}.json').write_text(json.dumps(first))
        summary, note = Path(d, 'summary.md'), Path(d, 'note.txt')
        out = io.StringIO()
        with redirect_stdout(out):
            status = ng.main(['nightly-gate.py', '--now', NOW, '--summary', str(summary),
                              '--note-file', str(note), *extra, d, *answers])
        read = lambda p: p.read_text() if p.exists() else ''  # noqa: E731
        return status, out.getvalue(), read(summary), read(note)


def all_green(**replace):
    answers = {name: (workflow(), GREEN) for name in ng.GATED}
    answers.update({name.replace('_', '-') + '.yml': value for name, value in replace.items()})
    return answers


class Verdicts(unittest.TestCase):
    def test_all_green_releases(self):
        status, out, summary, note = gate(all_green())
        self.assertEqual(status, 0)
        self.assertNotIn('::error::', out)
        self.assertEqual(summary.count('| pass |'), len(ng.GATED))
        self.assertEqual(note, '')

    def test_one_red_night_blocks_and_names_the_run(self):
        red = [run(3, '2026-11-14T02:43:00Z', 'failure')] + GREEN
        status, out, _, _ = gate(all_green(store_trace=(workflow(), red)))
        self.assertEqual(status, 1)
        self.assertIn('::error::nightly gate: store-trace.yml: the last scheduled run concluded failure: '
                      'https://example.invalid/runs/3', out)
        self.assertIn('not releasing', out)

    def test_timed_out_cancelled_and_startup_failure_block_like_failure(self):
        for conclusion in ['timed_out', 'cancelled', 'startup_failure']:
            with self.subTest(conclusion):
                status, out, _, _ = gate(all_green(
                    sim_sweep=(workflow(), [run(3, '2026-11-14T02:43:00Z', conclusion)])))
                self.assertEqual(status, 1)
                self.assertIn(f'concluded {conclusion}', out)

    def test_a_stale_night_blocks_even_when_it_was_green(self):
        stale = [run(1, '2026-11-12T11:59:00Z')]
        status, out, _, _ = gate(all_green(visibility_sweep=(workflow(), stale)))
        self.assertEqual(status, 1)
        self.assertIn('visibility-sweep.yml: the last completed scheduled run is 48 h old (limit 48 h)', out)

    def test_no_run_yet_passes_while_the_workflow_is_young(self):
        young = workflow(created='2026-11-13T20:00:00Z')
        status, out, _, _ = gate(all_green(store_trace=(young, [])))
        self.assertEqual(status, 0)
        self.assertIn('store-trace.yml has no completed scheduled run yet; it was added 16 h ago', out)

    def test_no_run_at_all_blocks_once_the_first_nights_should_have_run(self):
        status, out, _, _ = gate(all_green(store_trace=(workflow(), [])))
        self.assertEqual(status, 1)
        self.assertIn('store-trace.yml has no completed scheduled run on main although it was added', out)

    def test_a_run_in_progress_is_no_verdict_the_completed_one_before_it_decides(self):
        running = [run(3, '2026-11-14T02:43:00Z', None, 'in_progress')]
        status, out, _, _ = gate(all_green(sim_sweep=(workflow(), running + [run(2, '2026-11-13T02:43:00Z')])))
        self.assertEqual(status, 0)
        self.assertIn('a newer run is still in progress', out)
        status, _, _, _ = gate(all_green(sim_sweep=(workflow(), running + [
            run(2, '2026-11-13T02:43:00Z', 'failure')])))
        self.assertEqual(status, 1)

    def test_manual_runs_and_other_branches_do_not_count(self):
        runs = [run(4, '2026-11-14T10:00:00Z', event='workflow_dispatch'),
                run(3, '2026-11-14T09:00:00Z', branch='feature'),
                run(2, '2026-11-14T02:43:00Z', 'failure')]
        status, _, _, _ = gate(all_green(sim_sweep=(workflow(), runs)))
        self.assertEqual(status, 1)

    def test_a_disabled_workflow_blocks(self):
        status, out, _, _ = gate(all_green(sim_sweep=(workflow(state='disabled_manually'), GREEN)))
        self.assertEqual(status, 1)
        self.assertIn('sim-sweep.yml is disabled_manually', out)

    def test_an_unreadable_answer_is_bad_input_not_a_pass(self):
        with tempfile.TemporaryDirectory() as d:
            out = io.StringIO()
            with redirect_stdout(out):
                status = ng.main(['nightly-gate.py', '--now', NOW, d, 'sim-sweep.yml'])
        self.assertEqual(status, 2)
        self.assertIn('cannot read the answers', out.getvalue())
        self.assertIn('sim-sweep.yml.workflow.json', out.getvalue())


class DeletedAndRerunNights(unittest.TestCase):
    def test_a_deleted_run_after_the_deciding_one_blocks(self):
        # Run 3, the red night, was deleted; the green run 2 and a later manual run 4 remain.
        runs = [run(14, '2026-11-14T10:00:00Z', event='workflow_dispatch', number=4),
                run(12, '2026-11-14T02:43:00Z', number=2)]
        status, out, _, _ = gate(all_green(plain_sweep=(workflow(), runs)))
        self.assertEqual(status, 1)
        self.assertIn('plain-sweep.yml: run numbers 3 are missing after the last completed scheduled run '
                      '(a deleted run)', out)

    def test_runs_of_every_event_close_the_numbers_and_older_gaps_do_not_count(self):
        runs = [run(14, '2026-11-14T10:00:00Z', event='workflow_dispatch', number=8),
                run(13, '2026-11-14T09:00:00Z', branch='feature', number=7),
                run(12, '2026-11-14T02:43:00Z', number=6),
                run(5, '2026-11-10T02:43:00Z', 'failure', number=2)]
        status, _, _, _ = gate(all_green(plain_sweep=(workflow(), runs)))
        self.assertEqual(status, 0)

    def test_a_rerun_passes_only_when_its_first_attempt_succeeded_too(self):
        rerun = [run(3, '2026-11-14T02:43:00Z', attempt=2)] + GREEN
        status, out, summary, _ = gate(all_green(sim_sweep=(workflow(), rerun)),
                                       attempts={3: {'run_attempt': 1, 'conclusion': 'success'}})
        self.assertEqual(status, 0)
        self.assertIn('a re-run, attempt 2; the first attempt succeeded too', summary)

    def test_a_rerun_until_green_blocks(self):
        rerun = [run(3, '2026-11-14T02:43:00Z', attempt=3)] + GREEN
        status, out, _, _ = gate(all_green(sim_sweep=(workflow(), rerun)),
                                 attempts={3: {'run_attempt': 1, 'conclusion': 'failure'}})
        self.assertEqual(status, 1)
        self.assertIn('sim-sweep.yml: the last scheduled run is a re-run (attempt 3); its first attempt '
                      'concluded failure', out)

    def test_a_rerun_whose_first_attempt_is_unknown_blocks_and_says_so(self):
        rerun = [run(3, '2026-11-14T02:43:00Z', attempt=2)] + GREEN
        status, out, _, _ = gate(all_green(sim_sweep=(workflow(), rerun)))
        self.assertEqual(status, 1)
        self.assertIn('is a re-run (attempt 2) and its first attempt could not be read', out)


class Override(unittest.TestCase):
    RED = [run(3, '2026-11-14T02:43:00Z', 'failure')]

    def test_the_override_releases_and_leaves_its_reason_and_actor(self):
        with mock.patch.dict('os.environ', {'GITHUB_ACTOR': 'owner'}):
            status, out, summary, note = gate(all_green(sim_sweep=(workflow(), self.RED)),
                                              '--override', '  fix for an advisory\n')
        self.assertEqual(status, 0)
        self.assertNotIn('::error::', out)
        self.assertIn('::warning::nightly gate overridden by owner: fix for an advisory', out)
        self.assertIn('**Overridden** by owner. Reason: fix for an advisory', summary)
        self.assertIn('| sim-sweep.yml | block |', summary)
        self.assertEqual(note, 'Released by a manual run of `owner` despite a red, re-run, stale or missing '
                               'nightly sweep. Reason given: fix for an advisory\n')

    def test_the_release_note_mentions_nobody_and_carries_no_markup(self):
        with mock.patch.dict('os.environ', {'GITHUB_ACTOR': 'owner'}):
            _, _, _, note = gate(all_green(), '--override', 'ask @someone <!-- x --> `y`')
        self.assertEqual(note, 'Released by a manual run of `owner` with the nightly override given, though every '
                               'nightly sweep was green. Reason given: ask (at)someone !-- x -- y\n')
        self.assertNotIn('@', note)
        self.assertEqual(note.count('\n'), 1)

    def test_a_blank_override_is_no_override(self):
        status, _, _, note = gate(all_green(sim_sweep=(workflow(), self.RED)), '--override', ' \t ')
        self.assertEqual(status, 1)
        self.assertEqual(note, '')


def schedules():
    """{workflow file: [cron expressions]} of every workflow in .github/workflows."""
    found = {}
    for path in sorted((ROOT / '.github' / 'workflows').glob('*.y*ml')):
        crons = re.findall(r'^\s*-\s*cron:\s*["\']([^"\']+)["\']', path.read_text(), re.M)
        if crons:
            found[path.name] = crons
    return found


def daily(cron):
    return cron.split()[2:] == ['*', '*', '*']


class ScheduledWorkflows(unittest.TestCase):
    def test_every_scheduled_workflow_is_gated_or_excluded_with_a_reason(self):
        found = schedules()
        self.assertTrue(found)
        for name in found:
            with self.subTest(name):
                self.assertTrue(name in ng.GATED or ng.NOT_GATED.get(name, '').strip(),
                                f'{name} has a schedule: add it to GATED or to NOT_GATED with a reason')
        self.assertFalse(set(ng.GATED) & set(ng.NOT_GATED))
        for name in [*ng.GATED, *ng.NOT_GATED]:
            self.assertIn(name, found, f'{name} is listed but has no schedule (renamed or removed?)')

    def test_every_gated_workflow_runs_daily_so_the_48_hours_hold(self):
        found = schedules()
        for name in ng.GATED:
            self.assertTrue(any(daily(c) for c in found[name]), name)

    def test_release_yml_judges_exactly_the_gated_list(self):
        release = (ROOT / '.github' / 'workflows' / 'release.yml').read_text()
        self.assertIn('python3 scripts/nightly-gate.py --list', release)
        out = io.StringIO()
        with redirect_stdout(out):
            self.assertEqual(ng.main(['nightly-gate.py', '--list']), 0)
        self.assertEqual(out.getvalue().split(), list(ng.GATED))


if __name__ == '__main__':
    unittest.main()
