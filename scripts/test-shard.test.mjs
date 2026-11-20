import assert from 'node:assert/strict'
import { test } from 'node:test'
import { balance, fnv1a, parseList, partition, problems, runRegex, shardOf } from './test-shard.mjs'

const names = Array.from({ length: 400 }, (_, i) => `TestCase${i}`)

test('fnv1a matches the reference value', () => {
  assert.equal(fnv1a(''), 0x811c9dc5)
  assert.equal(fnv1a('a'), 0xe40c292c)
})

test('parseList keeps test names and drops the ok line', () => {
  const out = 'TestA\nTestB\nExampleC\nBenchmarkD\nok  \tpkg\t0.01s\n'
  assert.deepEqual(parseList(out), ['TestA', 'TestB', 'ExampleC'])
})

test('every test lands in exactly one shard, for any count', () => {
  for (const count of [1, 2, 3, 5]) {
    const shards = partition(names, count)
    assert.deepEqual(problems(names, shards), [])
    assert.equal(shards.flat().length, names.length)
  }
})

test('the split is deterministic and the halves are close to even', () => {
  assert.equal(shardOf('TestX', 2), shardOf('TestX', 2))
  const [a, b] = partition(names, 2)
  assert.ok(Math.abs(a.length - b.length) < 60, `${a.length} vs ${b.length}`)
})

test('problems reports a test in no shard and one in two', () => {
  const bad = problems(['TestA', 'TestB', 'TestC'], [['TestA', 'TestB'], ['TestB']])
  assert.deepEqual(bad, ['TestB: runs in 2 shards', 'TestC: runs in 0 shards'])
  assert.deepEqual(problems(['TestA'], [['TestA', 'TestGone']]), ['TestGone: not in the package'])
})

test('runRegex anchors the names and refuses an empty shard', () => {
  assert.equal(runRegex(['TestA', 'TestB']), '^(TestA|TestB)$')
  assert.throws(() => runRegex([]))
})

test('balance is deterministic, covers every item once and evens the load', () => {
  const weights = { a: 100, b: 90, c: 10, d: 10, e: 5 }
  const items = ['e', 'd', 'c', 'b', 'a', 'new']
  const shards = balance(items, weights, 2)
  assert.deepEqual(shards, balance([...items].reverse(), weights, 2))
  assert.deepEqual(problems(items, shards), [])
  const load = shards.map((s) => s.reduce((n, i) => n + (weights[i] ?? 10), 0))
  assert.ok(Math.abs(load[0] - load[1]) <= 20, `${load}`)
})
