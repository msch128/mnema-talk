import assert from 'node:assert/strict'
import { test } from 'node:test'
import configFactory from '../vite.config.ts'

test('vitest config isolates media and WASM tests in the forks pool', () => {
  const config = configFactory({ command: 'serve', mode: 'test' })
  assert.equal(config.test?.pool, 'forks')
  const projects = config.test?.projects as Array<{ test: { name: string; pool?: string; include?: string[]; exclude?: string[] } }> | undefined
  assert.ok(projects && projects.length === 2, 'expected two test projects')

  const media = projects.find(p => p.test.name === 'media')
  assert.ok(media, 'expected media project')
  assert.equal(media.test.pool, 'forks')
  assert.deepEqual(media.test.include, [
    'src/lib/media/**/*.test.{js,ts}',
    'src/lib/dfn/**/*.test.{js,ts}',
    'src/lib/noiseSuppressor.test.{js,ts}',
  ])

  const unit = projects.find(p => p.test.name === 'unit')
  assert.ok(unit, 'expected unit project')
  assert.deepEqual(unit.test.exclude, media.test.include)
})
