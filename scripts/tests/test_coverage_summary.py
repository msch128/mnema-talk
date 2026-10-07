"""Exercise the coverage gate's exact arithmetic and profile aggregation."""

import pathlib
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[2]


class CoverageSummaryTests(unittest.TestCase):
    def run_profile(self, blocks, minimum):
        with tempfile.TemporaryDirectory(prefix="mnema-coverage-summary-") as directory:
            profile = pathlib.Path(directory) / "coverage.out"
            profile.write_text("mode: atomic\n" + "\n".join(blocks) + "\n")
            return subprocess.run(
                ["node", str(ROOT / "scripts/coverage-summary.mjs"), "go", str(profile), "--min", str(minimum)],
                text=True,
                capture_output=True,
                check=False,
            )

    def test_rounded_hundred_still_rejects_an_uncovered_statement(self):
        result = self.run_profile([
            "github.com/msch128/mnema-talk/internal/example/main.go:1.1,2.1 2500 1",
            "github.com/msch128/mnema-talk/internal/example/main.go:3.1,4.1 1 0",
        ], 100)
        self.assertEqual(result.returncode, 1)
        self.assertIn("100.0 %", result.stdout)
        self.assertIn("2500 of 2501", result.stdout)
        self.assertIn("2500/2501", result.stderr)

    def test_rounding_does_not_lower_an_integer_threshold(self):
        result = self.run_profile([
            "github.com/msch128/mnema-talk/internal/example/main.go:1.1,2.1 1999 1",
            "github.com/msch128/mnema-talk/internal/example/main.go:3.1,4.1 501 0",
        ], 80)
        self.assertEqual(result.returncode, 1)
        self.assertIn("80.0 %", result.stdout)

    def test_exact_boundary_qualifies(self):
        result = self.run_profile([
            "github.com/msch128/mnema-talk/internal/example/main.go:1.1,2.1 2450 1",
            "github.com/msch128/mnema-talk/internal/example/main.go:3.1,4.1 50 0",
        ], 98)
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_shared_blocks_are_deduplicated_across_test_binaries(self):
        result = self.run_profile([
            "github.com/msch128/mnema-talk/internal/example/main.go:1.1,2.1 2 0",
            "github.com/msch128/mnema-talk/internal/example/main.go:1.1,2.1 2 1",
        ], 100)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("2 of 2", result.stdout)

    def test_empty_report_cannot_qualify_even_at_zero(self):
        result = self.run_profile([], 0)
        self.assertEqual(result.returncode, 1)


if __name__ == "__main__":
    unittest.main()
