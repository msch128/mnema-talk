#!/usr/bin/env python3
"""scripts/release-checks.py mirrors exactly what main's ruleset requires."""
import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location(
    'release_checks', Path(__file__).resolve().parents[1] / 'release-checks.py')
rc = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(rc)


def rules(*contexts):
    return [{'type': 'deletion'},
            {'type': 'required_status_checks',
             'parameters': {'required_status_checks': [{'context': c} for c in contexts]}}]


def run(name, conclusion, id_, status='completed'):
    return {'name': name, 'id': id_, 'status': status, 'conclusion': conclusion}


class ReleaseChecks(unittest.TestCase):
    def test_every_required_context_is_mirrored_including_new_ones(self):
        required = ['backend', 'upgrade-drill (1.0.0)', 'analyze (go)']
        runs = [run(c, 'success', i) for i, c in enumerate(required)] + [run('unrelated', 'failure', 9)]
        decisions = rc.decide(rules(*required), runs)
        self.assertEqual(decisions, [(c, 'success') for c in required])
        self.assertEqual(rc.exit_code(decisions), 0)

    def test_failed_required_check_blocks(self):
        decisions = rc.decide(rules('backend', 'e2e'), [run('backend', 'success', 1), run('e2e', 'failure', 2)])
        self.assertEqual(decisions, [('backend', 'success'), ('e2e', 'failure')])
        self.assertEqual(rc.exit_code(decisions), 1)

    def test_missing_or_running_check_is_pending_not_success(self):
        decisions = rc.decide(rules('backend', 'e2e', 'docker'),
                              [run('backend', 'success', 1), run('e2e', None, 2, 'in_progress')])
        self.assertEqual([s for _, s in decisions], ['success', 'pending', 'pending'])
        self.assertEqual(rc.exit_code(decisions), 3)

    def test_failure_wins_over_pending(self):
        decisions = rc.decide(rules('backend', 'e2e'), [run('backend', 'failure', 1)])
        self.assertEqual(rc.exit_code(decisions), 1)

    def test_rerun_uses_the_newest_run(self):
        decisions = rc.decide(rules('e2e'), [run('e2e', 'failure', 1), run('e2e', 'success', 2)])
        self.assertEqual(decisions, [('e2e', 'success')])
        decisions = rc.decide(rules('e2e'), [run('e2e', 'success', 1), run('e2e', 'cancelled', 2)])
        self.assertEqual(decisions, [('e2e', 'failure')])

    def test_skipped_and_neutral_pass(self):
        decisions = rc.decide(rules('a', 'b'), [run('a', 'skipped', 1), run('b', 'neutral', 2)])
        self.assertEqual(rc.exit_code(decisions), 0)

    def test_no_required_checks_refuses(self):
        self.assertEqual(rc.exit_code(rc.decide([{'type': 'deletion'}], [run('backend', 'success', 1)])), 1)

    def test_contexts_from_several_rules_are_deduplicated(self):
        self.assertEqual(rc.required_contexts(rules('a', 'b') + rules('b', 'c')), ['a', 'b', 'c'])


if __name__ == '__main__':
    unittest.main()
