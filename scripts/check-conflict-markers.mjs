#!/usr/bin/env node
// Fails when a tracked text file contains a merge-conflict marker at the start
// of a line (<<<<<<<, =======, >>>>>>>). Such markers are easy to commit by
// resolving a merge in a hurry, and nothing else notices them in docs or JSON.
// Usage: node scripts/check-conflict-markers.mjs
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const marker = /^(<{7}( |$)|={7}$|>{7}( |$))/

const tracked = execFileSync('git', ['ls-files', '-z'], { cwd: root, maxBuffer: 64 * 1024 * 1024 })
  .toString('utf8')
  .split('\0')
  .filter(Boolean)

const hits = []
for (const file of tracked) {
  let text
  try {
    const buf = readFileSync(join(root, file))
    if (buf.includes(0)) continue // binary
    text = buf.toString('utf8')
  } catch {
    continue // deleted in the working tree, or a submodule
  }
  text.split(/\r?\n/).forEach((line, i) => {
    if (marker.test(line)) hits.push(`${file}:${i + 1}: ${line.slice(0, 20)}`)
  })
}

if (hits.length > 0) {
  console.error('Merge-conflict markers found:\n' + hits.join('\n'))
  process.exit(1)
}
console.log(`no conflict markers in ${tracked.length} tracked files`)
