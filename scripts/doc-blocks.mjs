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
// Names used: caddy, nginx; setup-synology, setup-unraid,
// setup-vps, setup-flags; allow-old-version marks a block
// that deliberately names an old release (scripts/check-onboarding-docs.mjs).
//
// A name is looked up in README.md and every doc/*.md, so a block can move
// between docs without touching the checks; it must match exactly one block.
//
// CLI: node scripts/doc-blocks.mjs <name>   prints the block's text
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
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

// docFiles lists README.md and doc/*.md relative to the repository root.
export function docFiles(root = resolve(dirname(fileURLToPath(import.meta.url)), '..')) {
  return ['README.md', ...readdirSync(join(root, 'doc')).filter((f) => f.endsWith('.md')).sort().map((f) => `doc/${f}`)].map((f) => ({ file: f, path: join(root, f) }))
}

// findBlock returns the text of the one block called name across all docs.
export function findBlock(name, files = docFiles()) {
  const found = files.flatMap(({ file, path }) => markedBlocks(readFileSync(path, 'utf8')).filter((b) => b.name === name).map((b) => ({ ...b, file })))
  if (found.length !== 1) {
    throw new Error(`expected exactly one "<!-- test:${name} -->" block in README.md and doc/*.md, found ${found.length}` + found.map((b) => ` (${b.file}:${b.line})`).join(''))
  }
  return found[0].body
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const name = process.argv[2]
  if (!name) {
    console.error('usage: node scripts/doc-blocks.mjs <name>')
    process.exit(2)
  }
  try {
    process.stdout.write(findBlock(name))
  } catch (e) {
    console.error(e.message)
    process.exit(1)
  }
}
