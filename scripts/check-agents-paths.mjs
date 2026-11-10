#!/usr/bin/env node
// Fails when AGENTS.md names a file or directory that does not exist: every
// entry of the tree under "REPOSITORY STRUCTURE" and every path in backticks.
// Agents read that file first and trust it, so a stale path sends them looking
// for code that is gone.
//
// A backtick span counts as a path when it holds a slash and nothing but path
// characters (`internal/perm`, `doc/glossary.md`). Paths are relative to the
// repository root. Not checked: routes and slash commands (`/api/metrics`),
// commands and globs, Go module paths (`github.com/...`), remote refs
// (`origin/main`) and bare file names without a slash. A path that .gitignore
// covers (`data/`, `uploads/`) is named on purpose and passes.
// Usage: node scripts/check-agents-paths.mjs
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, join, posix, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const file = 'AGENTS.md'

const fence = /^```[^\n]*\n([\s\S]*?)^```[ \t]*$/gm
const treeEntry = /^((?:│ {3}| {4})*)[├└]── (.+)$/
const pathLike = /^[\w.@-]+(?:\/[\w.@-]+)*\/?$/
const hostname = /^[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$/

function lineOf(text, index) {
  return text.slice(0, index).split('\n').length
}

// The tree block is the first fenced block after the heading. An entry's depth
// is its indentation; a nested entry is relative to the directory above it.
// "a/b.sh, c.sh" names two files in one directory.
export function structurePaths(text) {
  const heading = text.search(/^## .*REPOSITORY STRUCTURE/m)
  if (heading < 0) return []
  fence.lastIndex = heading
  const block = fence.exec(text)
  if (!block) return []
  const first = lineOf(text, block.index) + 1
  const dirs = []
  const found = []
  block[1].split('\n').forEach((line, i) => {
    const m = treeEntry.exec(line)
    if (!m) return
    const depth = m[1].length / 4
    const names = m[2].split('#')[0].trim().split(/,\s*/)
    const parent = posix.join(...dirs.slice(0, depth), '.')
    const sibling = posix.dirname(names[0])
    names.forEach((name, n) => {
      const rel = n > 0 && !name.includes('/') ? posix.join(sibling, name) : name
      found.push({ path: posix.join(parent, rel), line: first + i })
    })
    dirs.length = depth
    if (names.length === 1 && names[0].endsWith('/')) dirs[depth] = names[0]
  })
  return found
}

export function backtickPaths(text) {
  const prose = text.replace(fence, (block) => block.replace(/[^\n]/g, ' '))
  const found = []
  for (const m of prose.matchAll(/`([^`\n]+)`/g)) {
    const span = m[1]
    if (!span.includes('/') || !pathLike.test(span)) continue
    const head = span.split('/')[0]
    if (hostname.test(head) || head === 'origin') continue
    found.push({ path: span, line: lineOf(prose, m.index) })
  }
  return found
}

// `exists` answers for a path relative to the repository root.
export function missingPaths(text, exists) {
  return [...structurePaths(text), ...backtickPaths(text)]
    .filter(({ path }) => !exists(path))
    .map(({ path, line }) => `${file}:${line}: ${path}`)
}

function ignored(path) {
  try {
    execFileSync('git', ['check-ignore', '-q', path], { cwd: root, stdio: 'ignore' })
    return true
  } catch {
    return false
  }
}

export function inRepository(path) {
  return existsSync(join(root, path)) || ignored(path)
}

function main() {
  const text = readFileSync(join(root, file), 'utf8')
  const total = structurePaths(text).length + backtickPaths(text).length
  const missing = missingPaths(text, inRepository)
  if (missing.length > 0) {
    console.error(`${file} names paths that do not exist:\n` + missing.join('\n'))
    console.error('\nFix the path, or remove the entry if the file is gone.')
    process.exit(1)
  }
  console.log(`${file}: ${total} paths checked`)
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()
