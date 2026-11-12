#!/usr/bin/env python3
"""scripts/nightly-gate.py: a red, stale or missing night blocks a release."""
from contextlib import redirect_stdout
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location(
    'nightly_gate', Path(__file__).resolve().parents[1] / 'nightly-gate.py')
ng = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ng)

NOW = '2026-11-14T12:00:00Z'
WORKFLOWS = ['sim-sweep.yml', 'visibility-sweep.yml', 'store-trace.yml']


def workflow(created='2026-11-01T03:43:37.000+01:00', state='active'):
    return {'id': 1, 'state': state, 'created_at': created}


def run(id_, created, conclusion='success', status='completed', event='schedule', branch='main'):
    return {'id': id_, 'created_at': created, 'status': status, 'conclusion': conclusion,
            'event': event, 'head_branch': branch, 'html_url': f'https://example.invalid/runs/{id_}'}


GREEN = [run(2, '2026-11-14T02:43:00Z'), run(1, '2026-11-13T02:43:00Z', 'failure')]


class Gate(unittest.TestCase):
    def gate(self, answers, *extra):
        """Runs the script on the given {file: (workflow, runs)} answers; returns (status, stdout, summary)."""
        with tempfile.TemporaryDirectory() as d:
            for name, (wf, runs) in answers.items():
                Path(d, f'{name}.workflow.json').write_text(json.dumps(wf))
                Path(d, f'{name}.runs.json').write_text(json.dumps({'total_count': len(runs), 'workflow_runs': runs}))
            summary = Path(d, 'summary.md')
            out = io.StringIO()
            with redirect_stdout(out):
                status = ng.main(['nightly-gate.py', '--now', NOW, '--summary', str(summary), *extra, d, *answers])
            return status, out.getvalue(), summary.read_text() if summary.exists() else ''

    def all_green(self, **replace):
        answers = {name: (workflow(), GREEN) for name in WORKFLOWS}
        answers.update({name.replace('_', '-') + '.yml': value for name, value in replace.items()})
        return answers

    def test_all_green_releases(self):
        status, out, summary = self.gate(self.all_green())
        self.assertEqual(status, 0)
        self.assertNotIn('::error::', out)
        self.assertEqual(summary.count('| pass |'), 3)

    def test_one_red_night_blocks_and_names_the_run(self):
        red = [run(3, '2026-11-14T02:43:00Z', 'failure')] + GREEN
        status, out, _ = self.gate(self.all_green(store_trace=(workflow(), red)))
        self.assertEqual(status, 1)
        self.assertIn('::error::nightly gate: store-trace.yml: the last scheduled run concluded failure: '
                      'https://example.invalid/runs/3', out)
        self.assertIn('not releasing', out)

    def test_timed_out_cancelled_and_startup_failure_block_like_failure(self):
        for conclusion in ['timed_out', 'cancelled', 'startup_failure']:
            with self.subTest(conclusion):
                status, out, _ = self.gate(self.all_green(
                    sim_sweep=(workflow(), [run(3, '2026-11-14T02:43:00Z', conclusion)])))
                self.assertEqual(status, 1)
                self.assertIn(f'concluded {conclusion}', out)

    def test_a_stale_night_blocks_even_when_it_was_green(self):
        stale = [run(1, '2026-11-12T11:59:00Z')]
        status, out, _ = self.gate(self.all_green(visibility_sweep=(workflow(), stale)))
        self.assertEqual(status, 1)
        self.assertIn('visibility-sweep.yml: the last completed scheduled run is 48 h old (limit 48 h)', out)

    def test_no_run_yet_passes_while_the_workflow_is_young(self):
        young = workflow(created='2026-11-13T20:00:00Z')
        status, out, _ = self.gate(self.all_green(store_trace=(young, [])))
        self.assertEqual(status, 0)
        self.assertIn('store-trace.yml has no completed scheduled run yet; it was added 16 h ago', out)

    def test_no_run_at_all_blocks_once_the_first_nights_should_have_run(self):
        status, out, _ = self.gate(self.all_green(store_trace=(workflow(), [])))
        self.assertEqual(status, 1)
        self.assertIn('store-trace.yml has no completed scheduled run on main although it was added', out)

    def test_a_run_in_progress_is_no_verdict_the_completed_one_before_it_decides(self):
        running = [run(3, '2026-11-14T02:43:00Z', None, 'in_progress')]
        status, out, _ = self.gate(self.all_green(sim_sweep=(workflow(), running + [
            run(2, '2026-11-13T02:43:00Z')])))
        self.assertEqual(status, 0)
        self.assertIn('a newer run is still in progress', out)
        status, _, _ = self.gate(self.all_green(sim_sweep=(workflow(), running + [
            run(2, '2026-11-13T02:43:00Z', 'failure')])))
        self.assertEqual(status, 1)

    def test_manual_runs_and_other_branches_do_not_count(self):
        runs = [run(4, '2026-11-14T10:00:00Z', event='workflow_dispatch'),
                run(3, '2026-11-14T09:00:00Z', branch='feature'),
                run(2, '2026-11-14T02:43:00Z', 'failure')]
        status, _, _ = self.gate(self.all_green(sim_sweep=(workflow(), runs)))
        self.assertEqual(status, 1)

    def test_a_disabled_workflow_blocks(self):
        status, out, _ = self.gate(self.all_green(sim_sweep=(workflow(state='disabled_manually'), GREEN)))
        self.assertEqual(status, 1)
        self.assertIn('sim-sweep.yml is disabled_manually', out)

    def test_the_override_releases_and_leaves_its_reason_and_actor(self):
        red = [run(3, '2026-11-14T02:43:00Z', 'failure')]
        answers = self.all_green(sim_sweep=(workflow(), red))
        with mock.patch.dict('os.environ', {'GITHUB_ACTOR': 'owner'}):
            status, out, summary = self.gate(answers, '--override', '  fix for an advisory\n')
        self.assertEqual(status, 0)
        self.assertNotIn('::error::', out)
        self.assertIn('::warning::nightly gate overridden by owner: fix for an advisory', out)
        self.assertIn('**Overridden** by owner. Reason: fix for an advisory', summary)
        self.assertIn('| sim-sweep.yml | block |', summary)

    def test_a_blank_override_is_no_override(self):
        red = [run(3, '2026-11-14T02:43:00Z', 'failure')]
        status, _, _ = self.gate(self.all_green(sim_sweep=(workflow(), red)), '--override', ' \t ')
        self.assertEqual(status, 1)

    def test_an_unreadable_answer_is_bad_input_not_a_pass(self):
        with tempfile.TemporaryDirectory() as d:
            out = io.StringIO()
            with redirect_stdout(out):
                status = ng.main(['nightly-gate.py', '--now', NOW, d, 'sim-sweep.yml'])
        self.assertEqual(status, 2)
        self.assertIn('cannot read the answers for sim-sweep.yml', out.getvalue())


if __name__ == '__main__':
    unittest.main()
