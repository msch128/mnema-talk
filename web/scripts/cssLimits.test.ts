import { test } from 'node:test'
import assert from 'node:assert/strict'
import { cssFiles, cssLimitViolations, MAX_CSS_LINES } from './cssLimits.ts'
import { fileURLToPath } from 'node:url'

test('a style sheet over the limit is reported, one at the limit is not', () => {
  const sheets: Record<string, string> = {
    'long.css': 'a{}\n'.repeat(MAX_CSS_LINES + 1),
    'exact.css': 'a{}\n'.repeat(MAX_CSS_LINES)
  }
  const found = cssLimitViolations(Object.keys(sheets), (f) => sheets[f] ?? '')
  assert.equal(found.length, 1)
  assert.match(found[0] ?? '', /long\.css: 401 lines/)
})

test('every style sheet of the app is within the limit', () => {
  const src = fileURLToPath(new URL('../src', import.meta.url))
  const files = cssFiles(src)
  assert.ok(files.length > 5)
  assert.deepEqual(cssLimitViolations(files), [])
})
