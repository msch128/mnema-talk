#!/usr/bin/env node
// Prints a Markdown coverage summary (total + least covered files) for the
// CI job summary. Usage:
//   node scripts/coverage-summary.mjs go coverage.out [--min 80]
//   node scripts/coverage-summary.mjs web web/coverage/coverage-summary.json [--min 80]
// With --min it exits 1 when the total is below that percentage. With
// --file-min it also exits 1 when any single file is below that percentage.
import { readFileSync } from 'node:fs'

const args = process.argv.slice(2)
const minAt = args.indexOf('--min')
const min = minAt === -1 ? null : Number(args.splice(minAt, 2)[1])
if (min !== null && !(min >= 0 && min <= 100)) {
  console.error('--min needs a percentage between 0 and 100')
  process.exit(2)
}
const fileMinAt = args.indexOf('--file-min')
const fileMin = fileMinAt === -1 ? null : Number(args.splice(fileMinAt, 2)[1])
if (fileMin !== null && !(fileMin >= 0 && fileMin <= 100)) {
  console.error('--file-min needs a percentage between 0 and 100')
  process.exit(2)
}
const [kind, file] = args
const LOWEST = 10
// Files below this many statements/lines are too small to be worth listing.
const MIN_SIZE = 30
// The per-file floor skips files this small (one-line main wrappers): one
// statement would swing them between 0 % and 100 %.
const FLOOR_MIN_SIZE = 5

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

const totalOk = min === null || (total.total > 0 && total.covered * 100 >= min * total.total)
const below = fileMin === null ? [] : files
  .filter(f => f.total >= FLOOR_MIN_SIZE && f.covered * 100 < fileMin * f.total)
  .sort((a, b) => a.covered / a.total - b.covered / b.total)
const gates = []
if (min !== null) gates.push(`${min} % in total`)
if (fileMin !== null) gates.push(`${fileMin} % in every file`)

const out = [
  `### ${title}: ${pct(total.covered, total.total)} %`,
  '',
  '| Scope | Covered | Total | % |',
  '|---|---:|---:|---:|',
  `| all files (${unit}) | ${total.covered} | ${total.total} | ${pct(total.covered, total.total)} % |`,
  '',
  `${total.covered} of ${total.total} ${unit} covered.`,
  ''
]
if (gates.length) out.push(`Gate: at least ${gates.join(' and ')}: **${totalOk && below.length === 0 ? 'pass' : 'fail'}**`, '')
out.push(
  `<details><summary>Lowest covered files (at least ${MIN_SIZE} ${unit})</summary>`,
  '',
  `| File | ${unit} | Covered | % |`,
  '|---|---:|---:|---:|'
)
files
  .filter(f => f.total >= MIN_SIZE)
  .sort((a, b) => a.covered / a.total - b.covered / b.total || b.total - a.total)
  .slice(0, LOWEST)
  .forEach(f => out.push(`| \`${f.name}\` | ${f.total} | ${f.covered} | ${pct(f.covered, f.total)} % |`))
out.push('', '</details>', '')
if (fileMin !== null) {
  out.push(below.length
    ? `Files below the ${fileMin} % floor: ${below.map(f => `\`${f.name}\` (${pct(f.covered, f.total)} %)`).join(', ')}`
    : `No file is below the ${fileMin} % floor.`, '')
}
console.log(out.join('\n'))

// Enforce the underlying counts: a rounded display of 100.0% must never
// qualify a profile with uncovered statements. Empty reports cannot qualify.
if (min !== null && (total.total === 0 || total.covered * 100 < min * total.total)) {
  const reason = total.total === 0
    ? 'the coverage report is empty'
    : `${total.covered}/${total.total} ${unit} covered; exact counts require at least ${min} %`
  console.error(`${title}: ${reason}`)
  process.exit(1)
}

if (below.length > 0) {
  console.error(`${title}: ${below.length} file(s) below the ${fileMin} % per-file floor:`)
  for (const f of below) console.error(`  ${f.name}: ${pct(f.covered, f.total)} % (${f.covered}/${f.total} ${unit})`)
  process.exit(1)
}
