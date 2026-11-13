import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { trimSlash } from '../src/util.ts'

describe('trimSlash', () => {
  test('drops trailing slashes only', () => {
    assert.equal(trimSlash('https://talk.example.org///'), 'https://talk.example.org')
    assert.equal(trimSlash('https://talk.example.org/path'), 'https://talk.example.org/path')
    assert.equal(trimSlash('///'), '')
    assert.equal(trimSlash(''), '')
  })

  test('takes linear time on 100,000 slashes followed by another character, and at the end', () => {
    const slashes = '/'.repeat(100_000)
    const start = performance.now()
    assert.equal(trimSlash(`${slashes}x`), `${slashes}x`)
    assert.equal(trimSlash(`x${slashes}`), 'x')
    const took = performance.now() - start
    assert.ok(took < 200, `took ${took} ms`)
  })
})
