import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { compact, extractBaseline, tagExists } from './compat-baseline.ts'

test('compact keeps the shape and drops the prose, flattening allOf', () => {
  const schema = compact({
    description: 'long text', example: 1, type: 'object', required: ['b', 'a'],
    properties: { a: { type: ['string', 'null'], format: 'uuid', description: 'x' }, list: { type: 'array', items: { $ref: '#/components/schemas/x.Y' } } },
    allOf: [{ properties: { c: { type: 'integer' } }, required: ['c'] }]
  })
  assert.deepEqual(schema, {
    type: 'object', required: ['a', 'b', 'c'],
    properties: { a: { type: ['string', 'null'], format: 'uuid' }, list: { type: 'array', items: { $ref: 'x.Y' } }, c: { type: 'integer' } }
  })
})

// The committed file must be what the extractor makes of the tag. A local
// shallow clone may carry no tags, so the test skips there; in CI (CI is set)
// the frontend job fetches the tag, and a missing tag is a failure, not a skip.
const inCI = Boolean(process.env['CI'])
test('the committed v2.9.12 baseline is what the extractor reads from the tag', { skip: !inCI && !tagExists('v2.9.12') && 'tag v2.9.12 is not in this checkout' }, () => {
  assert.ok(tagExists('v2.9.12'), 'tag v2.9.12 is not in this checkout: the CI job must fetch it (git fetch --depth 1 origin tag v2.9.12)')
  const committed = readFileSync(fileURLToPath(new URL('../src/compat/baselines/v2.9.12.json', import.meta.url)), 'utf8')
  assert.deepEqual(JSON.parse(committed), extractBaseline('v2.9.12'))
})
