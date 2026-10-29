#!/usr/bin/env node
// Docs as test input: returns the fenced code blocks that a Markdown file marks
// with an HTML comment on the line(s) before the opening fence:
//
//   <!-- test:caddy -->
//   ```caddy
//   chat.example.com { ... }
//   ```
//
// The marker can be indented (a block inside a list item); the block is
// returned dedented. The CI proxy matrix and setup assistant checks run the
// blocks as they stand, so editing a documented command changes what CI tests.
// Names used: caddy, nginx (doc/operations.md); setup-synology, setup-unraid,
// setup-vps, setup-flags (doc/quick-start.md); allow-old-version marks a block
// that deliberately names an old release (scripts/check-onboarding-docs.mjs).
//
// CLI: node scripts/doc-blocks.mjs <file.md> <name>   prints the block's text
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const MARKER = /^\s*<!--\s*test:([a-z0-9-]+)\s*-->\s*$/

// markedBlocks returns [{name, body, line}] for every marker followed by a fence.
export function markedBlocks(text) {
  const lines = text.split('\n')
  const out = []
  for (let i = 0; i < lines.length; i++) {
    const m = MARKER.exec(lines[i])
    if (!m) continue
    let j = i + 1
    while (j < lines.length && (lines[j].trim() === '' || MARKER.test(lines[j]))) j++
    const open = /^(\s*)(`{3,}|~{3,})[^\n]*$/.exec(lines[j] ?? '')
    if (!open) continue
    const indent = open[1].length
    const body = []
    let k = j + 1
    while (k < lines.length && !lines[k].trim().startsWith(open[2])) {
      body.push(lines[k].slice(Math.min(indent, lines[k].length - lines[k].trimStart().length)))
      k++
    }
    out.push({ name: m[1], body: body.join('\n') + '\n', line: i + 1, fenceLine: j + 1 })
  }
  return out
}

// markedBlock returns the text of the one block called name, or throws.
export function markedBlock(text, file, name) {
  const found = markedBlocks(text).filter((b) => b.name === name)
  if (found.length !== 1) {
    throw new Error(`${file}: expected exactly one "<!-- test:${name} -->" block, found ${found.length}`)
  }
  return found[0].body
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [file, name] = process.argv.slice(2)
  if (!file || !name) {
    console.error('usage: node scripts/doc-blocks.mjs <file.md> <name>')
    process.exit(2)
  }
  try {
    process.stdout.write(markedBlock(readFileSync(file, 'utf8'), file, name))
  } catch (e) {
    console.error(e.message)
    process.exit(1)
  }
}
