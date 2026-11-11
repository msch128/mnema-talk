#!/usr/bin/env node
// Compares a `go test -bench` run with the stored baseline and prints one row
// per benchmark of the run. Timing is shown and never judged: it depends on
// the machine and on what else runs there. With --gate-allocs <regex> the
// script exits 1 when a benchmark matching the regex allocates more per
// operation than its baseline, or is missing from the run or the baseline.
// Usage: node scripts/bench-compare.mjs <baseline.txt> <run.txt> [--gate-allocs <regex>]
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// parseBench reads the result lines of `go test -bench -benchmem` output:
// "BenchmarkName/sub-8  1000  123.4 ns/op  5.0 ns/viewer  16 B/op  1 allocs/op".
// The trailing -GOMAXPROCS is dropped so runs on different machines compare.
export function parseBench(text) {
  const results = new Map()
  for (const line of text.split('\n')) {
    const fields = line.trim().split(/\s+/)
    if (!/^Benchmark/.test(fields[0]) || !/^\d+$/.test(fields[1] ?? '')) continue
    const metrics = {}
    for (let i = 2; i + 1 < fields.length; i += 2) metrics[fields[i + 1]] = Number(fields[i])
    results.set(fields[0].replace(/-\d+$/, ''), metrics)
  }
  return results
}

function change(base, now) {
  if (base === undefined || now === undefined) return ''
  if (base === 0) return now === 0 ? '0%' : 'new'
  const pct = ((now - base) / base) * 100
  return `${pct >= 0 ? '+' : ''}${pct.toFixed(1)}%`
}

const show = (v) => (v === undefined ? '-' : String(v))

// compare returns the table rows and the failures of the allocation gate.
export function compare(baseline, run, gate) {
  const rows = [['benchmark', 'ns/op base', 'ns/op now', 'change', 'allocs/op base', 'allocs/op now']]
  const failures = []
  for (const [name, now] of run) {
    const base = baseline.get(name) ?? {}
    rows.push([name, show(base['ns/op']), show(now['ns/op']), change(base['ns/op'], now['ns/op']), show(base['allocs/op']), show(now['allocs/op'])])
    if (!gate?.test(name)) continue
    if (base['allocs/op'] === undefined) {
      failures.push(`${name}: no baseline (record one with make bench-baseline)`)
    } else if (now['allocs/op'] === undefined) {
      failures.push(`${name}: the run reports no allocations (run with -benchmem)`)
    } else if (now['allocs/op'] > base['allocs/op']) {
      failures.push(`${name}: ${now['allocs/op']} allocs/op, baseline ${base['allocs/op']}`)
    }
  }
  for (const name of baseline.keys()) {
    if (gate?.test(name) && !run.has(name)) failures.push(`${name}: in the baseline but not in the run`)
  }
  return { rows, failures }
}

export function table(rows) {
  const widths = rows[0].map((_, col) => Math.max(...rows.map((r) => r[col].length)))
  return rows.map((r) => r.map((cell, col) => (col === 0 ? cell.padEnd(widths[col]) : cell.padStart(widths[col]))).join('  ')).join('\n')
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2)
  const gateAt = args.indexOf('--gate-allocs')
  const gate = gateAt === -1 ? null : new RegExp(args.splice(gateAt, 2)[1])
  const [baselineFile, runFile] = args
  if (!baselineFile || !runFile) {
    console.error('usage: bench-compare.mjs <baseline.txt> <run.txt> [--gate-allocs <regex>]')
    process.exit(2)
  }
  const baselineText = readFileSync(baselineFile, 'utf8')
  const { rows, failures } = compare(parseBench(baselineText), parseBench(readFileSync(runFile, 'utf8')), gate)
  console.log('baseline:')
  for (const line of baselineText.split('\n')) if (line.startsWith('#')) console.log(`  ${line.slice(1).trim()}`)
  console.log(`\n${table(rows)}\n`)
  console.log('Timing is not judged: compare it only with a baseline from the same idle machine.')
  if (failures.length > 0) {
    console.error(`\nmore allocations per operation than the baseline:\n${failures.map((f) => `  ${f}`).join('\n')}`)
    process.exit(1)
  }
}
