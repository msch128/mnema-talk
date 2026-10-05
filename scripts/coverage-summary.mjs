#!/usr/bin/env node
// Prints a Markdown coverage summary (total + least covered files) for the
// CI job summary. Usage:
//   node scripts/coverage-summary.mjs go coverage.out
//   node scripts/coverage-summary.mjs web web/coverage/coverage-summary.json
import { readFileSync } from 'node:fs'

const [kind, file] = process.argv.slice(2)
const LOWEST = 15
// Files below this many statements/lines are too small to be worth listing.
const MIN_SIZE = 30

function fromGoProfile(path) {
  // Profile lines: <file>:<start>,<end> <statements> <count>. With -coverpkg
  // the same block appears once per test binary, so keep the highest count.
  const blocks = new Map()
  for (const line of readFileSync(path, 'utf8').split('\n').slice(1)) {
    const m = line.match(/^(.+):(\S+) (\d+) (\d+)$/)
    if (!m) continue
    const key = `${m[1]}:${m[2]}`
    const prev = blocks.get(key)
    blocks.set(key, { file: m[1], n: Number(m[3]), hit: (prev?.hit ?? false) || Number(m[4]) > 0 })
  }
  const files = new Map()
  for (const { file, n, hit } of blocks.values()) {
    const f = files.get(file) ?? { total: 0, covered: 0 }
    f.total += n
    if (hit) f.covered += n
    files.set(file, f)
  }
  const prefix = /^github\.com\/[^/]+\/[^/]+\//
  return { unit: 'statements', files: [...files].map(([name, f]) => ({ name: name.replace(prefix, ''), ...f })) }
}

function fromVitestSummary(path) {
  const summary = JSON.parse(readFileSync(path, 'utf8'))
  const files = Object.entries(summary)
    .filter(([name]) => name !== 'total')
    .map(([name, s]) => ({ name: name.replace(/^.*?\/web\//, 'web/'), total: s.lines.total, covered: s.lines.covered }))
  return { unit: 'lines', files }
}

const { unit, files } = kind === 'go' ? fromGoProfile(file) : fromVitestSummary(file)
const pct = (c, t) => (t ? (100 * c / t).toFixed(1) : '0.0')
const total = files.reduce((a, f) => ({ total: a.total + f.total, covered: a.covered + f.covered }), { total: 0, covered: 0 })
const title = kind === 'go' ? 'Go coverage (unit + integration)' : 'Web coverage (vitest)'

const out = [
  `### ${title}: ${pct(total.covered, total.total)} %`,
  '',
  `${total.covered} of ${total.total} ${unit} covered.`,
  '',
  `<details><summary>Least covered files (at least ${MIN_SIZE} ${unit})</summary>`,
  '',
  `| File | Coverage | ${unit} |`,
  '|---|---:|---:|'
]
files
  .filter(f => f.total >= MIN_SIZE)
  .sort((a, b) => a.covered / a.total - b.covered / b.total || b.total - a.total)
  .slice(0, LOWEST)
  .forEach(f => out.push(`| \`${f.name}\` | ${pct(f.covered, f.total)} % | ${f.total} |`))
out.push('', '</details>', '')
console.log(out.join('\n'))
