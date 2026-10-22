import assert from 'node:assert/strict'
import { test } from 'node:test'
import { shellFiles, shellVersion } from './serviceWorkerBuild.ts'

const page = `<!DOCTYPE html><html><head>
<link rel="icon" href="/favicon.svg" />
<script type="module" crossorigin src="/assets/main-AbC123.js"></script>
<link rel="stylesheet" crossorigin href="/assets/main-XyZ789.css">
<link rel="modulepreload" href="/assets/main-AbC123.js">
</head></html>`

test('the shell lists the page, what it loads from /assets/ once, and the fixed icons', () => {
  const files = shellFiles(page)
  assert.deepEqual(files.slice(0, 3), ['/', '/assets/main-AbC123.js', '/assets/main-XyZ789.css'])
  assert.ok(files.includes('/manifest.webmanifest'))
  assert.ok(files.includes('/pwa-icon-192.png'))
  assert.equal(files.filter(f => f === '/assets/main-AbC123.js').length, 1)
})

test('a build is named after its files: same files, same name; other files, other name', () => {
  const files = shellFiles(page)
  assert.equal(shellVersion(files), shellVersion([...files]))
  assert.notEqual(shellVersion(files), shellVersion(shellFiles(page.replace('AbC123', 'Q1w2e3'))))
  assert.match(shellVersion(files), /^[0-9a-f]{12}$/)
})
