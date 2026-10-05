#!/usr/bin/env node
// Prints the weighted 1.0 score from docs/SCORECARD.md.
// Areas are "## Name (weight N)" headings, items are "- [x]" / "- [ ]" lines.
import { readFileSync } from 'node:fs'

const file = new URL('../docs/SCORECARD.md', import.meta.url)
const areas = []
for (const line of readFileSync(file, 'utf8').split('\n')) {
  const head = line.match(/^## (.+) \(weight (\d+)\)$/)
  if (head) {
    areas.push({ name: head[1], weight: Number(head[2]), done: 0, total: 0 })
    continue
  }
  const item = line.match(/^- \[( |x)\] /)
  if (item && areas.length) {
    const area = areas.at(-1)
    area.total++
    if (item[1] === 'x') area.done++
  }
}

const weights = areas.reduce((s, a) => s + a.weight, 0)
let total = 0
for (const a of areas) {
  const pct = a.total ? (a.done / a.total) * 100 : 0
  total += (pct * a.weight) / weights
  console.log(`${a.name.padEnd(28)} ${String(a.done).padStart(2)}/${String(a.total).padEnd(2)} ${pct.toFixed(0).padStart(3)} %  (weight ${a.weight})`)
}
console.log(`${'Total'.padEnd(28)}       ${total.toFixed(1)} %`)
