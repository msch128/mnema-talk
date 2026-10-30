import assert from 'node:assert/strict'
import { test } from 'node:test'
import { markedBlock, markedBlocks } from './doc-blocks.mjs'
import { comparePorts, composePorts, docPorts, missingCodes, staleVersions } from './check-onboarding-docs.mjs'

const cur = { major: 2, minor: 0 }

test('a release PR cannot turn the check red: it does not read version.txt', async () => {
  // Simulates version.txt = 2.2.0 (and 3.0.0): tags that were current stay valid.
  const { readFileSync } = await import('node:fs')
  assert.doesNotMatch(readFileSync(new URL('./check-onboarding-docs.mjs', import.meta.url), 'utf8'), /read\('version\.txt'\)/)
  for (const tag of ['2.1', '2.1.1', '2.2', '3.0']) {
    assert.equal(staleVersions('a.md', '```sh\nMNEMA_IMAGE=ghcr.io/msch128/mnema-talk:' + tag + '\n```\n').length, 0, tag)
  }
})

test('a stale image tag in a command block fails, a current or latest tag passes', () => {
  const md = (tag) => '```sh\nMNEMA_IMAGE=ghcr.io/msch128/mnema-talk:' + tag + '\n```\n'
  assert.equal(staleVersions('a.md', md('1.4'), cur).length, 1)
  assert.equal(staleVersions('a.md', md('1.9.9'), cur).length, 1)
  assert.equal(staleVersions('a.md', md('2.1'), cur).length, 0)
  assert.equal(staleVersions('a.md', md('2.1.1'), cur).length, 0)
  assert.equal(staleVersions('a.md', md('latest'), cur).length, 0)
})

test('prose is not checked, an allow marker exempts a block, VERSION= is checked', () => {
  assert.equal(staleVersions('a.md', 'Use `mnema-talk:1.0` once.\n', cur).length, 0)
  assert.equal(staleVersions('a.md', '<!-- test:allow-old-version -->\n```sh\nimage mnema-talk:0.4.4\n```\n', cur).length, 0)
  assert.equal(staleVersions('a.md', '```sh\nVERSION=1.1.0\n```\n', cur).length, 1)
})

test('every code must appear in backticks in a doc', () => {
  assert.equal(missingCodes(['a.ok', 'a.bad'], ['| `a.ok` | fine |']).length, 1)
  assert.match(missingCodes(['a.bad'], [''])[0], /"a\.bad".*doc\/operations\.md/)
})

const compose = `services:
  app:
    ports:
      # comment
      - "\${PUBLISH_ADDR:-127.0.0.1}:\${PORT:-8080}:8080"
      - "\${LO:-50000}-\${HI:-50050}:\${LO:-50000}-\${HI:-50050}/udp"
      - "\${T:-0.0.0.0}:\${P:-3479}:\${P:-3479}/tcp"
  coturn:
    command: --listening-port=3478 --min-port=49152 --max-port=49200 \\
`

test('ports are read from compose with their defaults and from the coturn flags', () => {
  const keys = [...composePorts(compose).keys()].sort()
  assert.deepEqual(keys, ['3478/tcp', '3478/udp', '3479/tcp', '49152-49200/udp', '50000-50050/udp', '8080/tcp'])
})

test('ports differ in both directions with a message naming the port', () => {
  const doc = '## Inbound ports\n\n| Port | Protocol | From | Default | Purpose | Needed | Open |\n|---|---|---|---|---|---|---|\n' +
    '| 443 | TCP | Internet | Your reverse proxy | web | yes | yes |\n| 8080 | TCP | Proxy | `PORT` | app | yes | no |\n| 9999 | UDP | Internet | x | y | z | w |\n'
  const problems = comparePorts(composePorts(compose), docPorts(doc))
  assert.ok(problems.some((p) => p.startsWith('port 3479/tcp is published')))
  assert.ok(problems.some((p) => p.startsWith('port 9999/udp is listed')))
  assert.ok(!problems.some((p) => p.includes('443') || p.includes('8080/tcp')))
})

test('marked blocks are found, dedented and need exactly one match', () => {
  const md = '1. step\n\n   <!-- test:x -->\n   ```sh\n   echo a\n     echo b\n   ```\n'
  assert.equal(markedBlock(md, 'f.md', 'x'), 'echo a\n  echo b\n')
  assert.throws(() => markedBlock(md, 'f.md', 'y'), /exactly one/)
  assert.equal(markedBlocks('<!-- test:a -->\nno fence here\n').length, 0)
})
