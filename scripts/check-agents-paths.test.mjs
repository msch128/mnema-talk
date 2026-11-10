import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { backtickPaths, inRepository, missingPaths, structurePaths } from './check-agents-paths.mjs'

const agents = readFileSync(new URL('../AGENTS.md', import.meta.url), 'utf8')
const paths = (found) => found.map((p) => p.path)

const tree = [
  '## REPOSITORY STRUCTURE',
  '',
  '```',
  'project/',
  '├── cmd/server/          # Entrypoint',
  '├── internal/',
  '│   ├── auth/            # Sign-in; authn/ holds the handlers',
  '│   └── tools/fix/       # Post-processing',
  '├── web/                 # The app',
  '│   └── src/lib/         # Shared modules',
  '├── scripts/backup.sh, restore.sh # Backup and restore',
  '├── .github/workflows/',
  '│   ├── ci.yml           # CI: lint, tests,',
  '│   │                    # docker build',
  '│   └── release.yml      # CD',
  '├── a.json, b.txt',
  '│                        # Managed elsewhere',
  '└── README.md            # Overview',
  '```',
].join('\n')

test('the checked-in AGENTS.md names only paths that exist', () => {
  assert.deepEqual(missingPaths(agents, inRepository), [])
})

test('a wrong path in backticks is reported with its line', () => {
  const text = agents + '\nSee `web/src/stores/no-such-store.js` for the store.\n'
  const line = text.split('\n').findIndex((l) => l.includes('no-such-store')) + 1
  assert.deepEqual(missingPaths(text, inRepository), [`AGENTS.md:${line}: web/src/stores/no-such-store.js`])
})

test('a wrong entry in the structure tree is reported', () => {
  const text = agents.replace('│   ├── perm/ ', '│   ├── perm/ \n│   ├── no-such-package/ ')
  assert.notEqual(text, agents)
  assert.deepEqual(
    missingPaths(text, inRepository).map((p) => p.split(': ')[1]),
    ['internal/no-such-package/'],
  )
})

test('a directory named as a file, or a file named as a directory, is wrong', () => {
  assert.equal(missingPaths('`scripts/backup.sh/`', inRepository).length, 1)
  assert.equal(missingPaths('`scripts/backup.sh` and `scripts/`', inRepository).length, 0)
})

test('tree entries resolve against the directory above them', () => {
  assert.deepEqual(paths(structurePaths(tree)), [
    'cmd/server/',
    'internal/',
    'internal/auth/',
    'internal/tools/fix/',
    'web/',
    'web/src/lib/',
    'scripts/backup.sh',
    'scripts/restore.sh',
    '.github/workflows/',
    '.github/workflows/ci.yml',
    '.github/workflows/release.yml',
    'a.json',
    'b.txt',
    'README.md',
  ])
})

test('tree entries carry their line in the file', () => {
  const found = structurePaths(tree)
  assert.equal(found[0].line, 5)
  assert.equal(found.at(-1).line, 18)
})

test('only spans that look like a repository path are checked', () => {
  const text = [
    'Run `make check` and `go test ./...`, see `/api/metrics` and `/poll`.',
    'Uses `github.com/pion/webrtc/v4`; rebase onto `origin/main`.',
    'No `*.db`, no `feat:`; `.ts`/`.vue` files; bare `release.yml`.',
    'Read `internal/perm` and `doc/glossary.md` and `web/src/third_party/`.',
  ].join('\n')
  assert.deepEqual(paths(backtickPaths(text)), ['internal/perm', 'doc/glossary.md', 'web/src/third_party/'])
})

test('backticks inside a fenced block are not read as paths', () => {
  assert.deepEqual(backtickPaths('```\n├── api/ # Generated (`make/openapi`)\n```\n'), [])
})

test('a path that .gitignore covers passes without existing', () => {
  assert.deepEqual(missingPaths('Never stage `data/` or `uploads/`.', inRepository), [])
})
