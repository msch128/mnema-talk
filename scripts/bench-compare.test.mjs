import assert from 'node:assert/strict'
import { test } from 'node:test'
import { compare, parseBench } from './bench-compare.mjs'

const baseline = parseBench(`# machine: example
goos: linux
BenchmarkForwardPlainTrack/viewers=20-8   1000   200.0 ns/op   10.00 ns/viewer   0 B/op   0 allocs/op
BenchmarkForwardLayeredTrack/viewers=20-8   1000   400.0 ns/op   20.00 ns/viewer   16 B/op   1 allocs/op
BenchmarkBroadcastChannel/clients=1000-8   100   90000 ns/op   2048 B/op   12 allocs/op
PASS
`)
const gate = /^BenchmarkForward/

test('results are read without the GOMAXPROCS suffix', () => {
  assert.deepEqual(baseline.get('BenchmarkForwardPlainTrack/viewers=20'), { 'ns/op': 200, 'ns/viewer': 10, 'B/op': 0, 'allocs/op': 0 })
  assert.equal(baseline.size, 3)
})

test('slower but not allocating more passes the gate', () => {
  const run = parseBench(`BenchmarkForwardPlainTrack/viewers=20-4 10 900.0 ns/op 0 B/op 0 allocs/op
BenchmarkForwardLayeredTrack/viewers=20-4 10 900.0 ns/op 0 B/op 0 allocs/op`)
  const { rows, failures } = compare(baseline, run, gate)
  assert.deepEqual(failures, [])
  assert.deepEqual(rows[1], ['BenchmarkForwardPlainTrack/viewers=20', '200', '900', '+350.0%', '0', '0'])
})

test('more allocations on a gated benchmark fail', () => {
  const run = parseBench(`BenchmarkForwardPlainTrack/viewers=20-4 10 100.0 ns/op 8 B/op 1 allocs/op
BenchmarkForwardLayeredTrack/viewers=20-4 10 100.0 ns/op 16 B/op 1 allocs/op`)
  assert.deepEqual(compare(baseline, run, gate).failures, ['BenchmarkForwardPlainTrack/viewers=20: 1 allocs/op, baseline 0'])
})

test('more allocations outside the gate do not fail', () => {
  const run = parseBench(`BenchmarkBroadcastChannel/clients=1000-4 10 100.0 ns/op 8 B/op 99 allocs/op`)
  assert.deepEqual(compare(baseline, run, null).failures, [])
})

test('a gated benchmark missing on either side fails', () => {
  const run = parseBench(`BenchmarkForwardPlainTrack/viewers=20-4 10 100.0 ns/op 0 B/op 0 allocs/op
BenchmarkForwardNew-4 10 100.0 ns/op 0 B/op 0 allocs/op`)
  assert.deepEqual(compare(baseline, run, gate).failures, [
    'BenchmarkForwardNew: no baseline (record one with make bench-baseline)',
    'BenchmarkForwardLayeredTrack/viewers=20: in the baseline but not in the run',
  ])
})
