#!/usr/bin/env node
// Splits the tests of one Go package into shards that CI runs in parallel jobs.
// internal/server alone outgrew the 15 minute test timeout, and the timeout
// stays where it is: the package is cut into two halves instead.
//
// A test lands in a shard by a hash of its name (FNV-1a), so the split is
// deterministic, needs no list to maintain and a new test is always picked up.
// With hundreds of tests the halves come out close to even.
//
// Usage:
//   node scripts/test-shard.mjs run   <index>/<count> <package> [tags]
//       prints the -run regex that selects this shard's tests
//   node scripts/test-shard.mjs check <count> <package> [tags]
//       fails unless every test is in exactly one of the <count> shards
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const testName = /^(Test|Fuzz|Example)/

export function fnv1a(name) {
  let h = 0x811c9dc5
  for (const byte of Buffer.from(name, 'utf8')) {
    h ^= byte
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return h
}

// shardOf returns the 1-based shard a test belongs to.
export function shardOf(name, count) {
  return (fnv1a(name) % count) + 1
}

// parseList reads `go test -list` output: one name per line, then an "ok" line.
export function parseList(output) {
  return output.split('\n').map((l) => l.trim()).filter((l) => testName.test(l))
}

export function partition(names, count) {
  const shards = Array.from({ length: count }, () => [])
  for (const name of names) shards[shardOf(name, count) - 1].push(name)
  return shards
}

// runRegex anchors every name: -run matches the top-level test name unanchored.
export function runRegex(names) {
  if (names.length === 0) throw new Error('shard has no tests')
  return `^(${names.join('|')})$`
}

// problems reports tests that run in no shard or in more than one, given what
// each shard would run (as lists of names) and the full list.
export function problems(all, shards) {
  const seen = new Map()
  for (const shard of shards) for (const name of shard) seen.set(name, (seen.get(name) ?? 0) + 1)
  const out = []
  for (const name of all) {
    const n = seen.get(name) ?? 0
    if (n !== 1) out.push(`${name}: runs in ${n} shards`)
  }
  for (const name of seen.keys()) if (!all.includes(name)) out.push(`${name}: not in the package`)
  return out
}

function listTests(pkg, tags) {
  const args = ['test', '-list', '.*', ...(tags ? [`-tags=${tags}`] : []), pkg]
  return parseList(execFileSync('go', args, { encoding: 'utf8', maxBuffer: 64 << 20 }))
}

function main(argv) {
  const [mode, ...rest] = argv
  if (mode === 'run') {
    const [spec, pkg, tags] = rest
    const m = /^(\d+)\/(\d+)$/.exec(spec ?? '')
    if (!m || !pkg) throw new Error('usage: run <index>/<count> <package> [tags]')
    const [index, count] = [Number(m[1]), Number(m[2])]
    if (index < 1 || index > count) throw new Error(`shard ${spec} is out of range`)
    console.log(runRegex(partition(listTests(pkg, tags), count)[index - 1]))
    return
  }
  if (mode === 'check') {
    const [countArg, pkg, tags] = rest
    const count = Number(countArg)
    if (!Number.isInteger(count) || count < 1 || !pkg) throw new Error('usage: check <count> <package> [tags]')
    const all = listTests(pkg, tags)
    const shards = partition(all, count)
    const bad = problems(all, shards)
    if (bad.length > 0) {
      console.error(bad.join('\n'))
      process.exit(1)
    }
    console.log(`${pkg}: ${all.length} tests, one shard each (${shards.map((s) => s.length).join(' + ')})`)
    return
  }
  throw new Error('usage: test-shard.mjs run|check ...')
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2))
  } catch (err) {
    console.error(err.message)
    process.exit(2)
  }
}
