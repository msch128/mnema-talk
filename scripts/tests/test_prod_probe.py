#!/usr/bin/env python3
"""scripts/prod-probe.sh against a local fake server (plain http: no TLS check)."""
import datetime
import http.server
import json
import os
from pathlib import Path
import subprocess
import threading
import unittest

ROOT = Path(os.environ.get('DRILL_TEST_ROOT', Path(__file__).resolve().parents[2]))
PROBE = ROOT / 'scripts' / 'prod-probe.sh'


def iso(hours_ago):
    t = datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(hours=hours_ago)
    return t.strftime('%Y-%m-%dT%H:%M:%SZ')


class Server:
    def __init__(self, health=(200, {'status': 'ok', 'version': '3.3.5'}), page=(200, '<html></html>'),
                 release=(200, {'tag_name': 'v3.3.5', 'published_at': iso(100)})):
        routes = {'/api/health': health, '/': page, '/release': release}
        self.hits = {}
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                outer.hits[self.path] = outer.hits.get(self.path, 0) + 1
                code, body = routes[self.path]
                data = body if isinstance(body, str) else json.dumps(body)
                self.send_response(code)
                self.send_header('Content-Type', 'text/html' if isinstance(body, str) else 'application/json')
                self.end_headers()
                self.wfile.write(data.encode())

            def log_message(self, *args):
                pass

        self.httpd = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.url = f'http://127.0.0.1:{self.httpd.server_address[1]}'
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


def probe(server, **env):
    full = dict(os.environ, PROBE_RETRY_DELAY='0', PROBE_RELEASE_URL=server.url + '/release', **env)
    return subprocess.run([str(PROBE), server.url], capture_output=True, text=True, env=full, timeout=60)


class ProdProbe(unittest.TestCase):
    def serve(self, **kw):
        s = Server(**kw)
        self.addCleanup(s.close)
        return s

    def test_healthy_server_passes(self):
        res = probe(self.serve())
        self.assertEqual(res.returncode, 0, res.stdout + res.stderr)
        self.assertIn('running 3.3.5', res.stdout)

    def test_health_failure_is_retried_three_times_then_reported(self):
        s = self.serve(health=(502, {'error': 'bad gateway'}))
        res = probe(s)
        self.assertEqual(res.returncode, 1)
        self.assertEqual(s.hits['/api/health'], 3)
        self.assertIn('/api/health', res.stdout)

    def test_status_not_ok_is_a_failure(self):
        res = probe(self.serve(health=(200, {'status': 'degraded', 'version': '3.3.5'})))
        self.assertEqual(res.returncode, 1)

    def test_missing_web_client_is_a_failure(self):
        res = probe(self.serve(page=(200, 'not a page')))
        self.assertEqual(res.returncode, 1)
        self.assertIn('web client', res.stdout)

    def test_old_version_after_the_grace_period_means_stuck_updater(self):
        s = self.serve(release=(200, {'tag_name': 'v3.4.0', 'published_at': iso(10)}))
        res = probe(s)
        self.assertEqual(res.returncode, 1)
        self.assertIn('self-updater is stuck', res.stdout)

    def test_new_release_inside_the_grace_period_is_fine(self):
        s = self.serve(release=(200, {'tag_name': 'v3.4.0', 'published_at': iso(1)}))
        self.assertEqual(probe(s).returncode, 0)

    def test_unreachable_release_api_skips_only_that_check(self):
        s = self.serve(release=(500, {}))
        res = probe(s)
        self.assertEqual(res.returncode, 0)
        self.assertIn('skipped', res.stderr)

    def test_bad_usage(self):
        res = subprocess.run([str(PROBE), 'talk.example'], capture_output=True, text=True, timeout=10)
        self.assertEqual(res.returncode, 2)


if __name__ == '__main__':
    unittest.main()
