import assert from 'node:assert/strict'
import { test } from 'node:test'
import { germanLines, parseAllowlist } from './check-english-only.mjs'

test('flags umlauts and German words, not English text', () => {
  assert.equal(germanLines('// Nachricht senden\nconst a = 1').length, 1)
  assert.equal(germanLines('name: "Zürich"').length, 1)
  assert.equal(germanLines('// the message is sent once, in order').length, 0)
  assert.equal(germanLines('const unmitigated = auf2').length, 0)
})

test('text entries remove names before the check', () => {
  const { texts } = parseAllowlist('text: Schr(ö|oe)der | a surname')
  assert.equal(germanLines('Marius Schröder', texts).length, 0)
  assert.equal(germanLines('Marius Schröder und ich', texts).length, 1)
})

test('an allowlist entry without a reason is rejected', () => {
  assert.throws(() => parseAllowlist('path: web/** |'))
  assert.throws(() => parseAllowlist('path: web/**'))
})

test('globs: * stays in a folder, ** crosses folders', () => {
  const { paths } = parseAllowlist('path: a/*.ts | one\npath: b/** | two')
  assert.ok(paths[0].test('a/x.ts') && !paths[0].test('a/c/x.ts'))
  assert.ok(paths[1].test('b/c/d.ts'))
})
