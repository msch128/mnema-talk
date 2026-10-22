#!/usr/bin/env node
// Checks relative links in README.md, SECURITY.md and doc/*.md: the target
// file must exist and a #fragment must match a heading of the target (GitHub's
// slug rules). No network access; http(s) and mailto links are ignored.
// Usage: node scripts/check-doc-links.mjs
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const files = [
  'README.md',
  'SECURITY.md',
  ...readdirSync(join(root, 'doc'))
    .filter((f) => f.endsWith('.md'))
    .map((f) => `doc/${f}`),
]

function withoutFences(text) {
  return text.replace(/^(```|~~~)[\s\S]*?^\1/gm, '')
}

function withoutCode(text) {
  return withoutFences(text).replace(/`[^`\n]*`/g, '')
}

function slug(heading) {
  return heading
    .trim()
    .toLowerCase()
    .replace(/`/g, '')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/[^\p{L}\p{N}\s_-]/gu, '')
    .replace(/\s/g, '-')
}

const anchorCache = new Map()
function anchors(path) {
  if (!anchorCache.has(path)) {
    const seen = new Map()
    const set = new Set()
    for (const m of withoutFences(readFileSync(path, 'utf8')).matchAll(/^#{1,6}\s+(.+?)\s*#*$/gm)) {
      const base = slug(m[1])
      const n = seen.get(base) ?? 0
      seen.set(base, n + 1)
      set.add(n === 0 ? base : `${base}-${n}`)
    }
    anchorCache.set(path, set)
  }
  return anchorCache.get(path)
}

const problems = []
for (const file of files) {
  const path = join(root, file)
  const text = withoutCode(readFileSync(path, 'utf8'))
  for (const m of text.matchAll(/!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
    const target = m[1]
    if (/^([a-z][a-z0-9+.-]*:|\/\/)/i.test(target)) continue
    const [rawFile, fragment] = target.split('#')
    const dest = rawFile === '' ? path : resolve(dirname(path), decodeURIComponent(rawFile))
    if (!existsSync(dest)) {
      problems.push(`${file}: missing file for (${target})`)
    } else if (fragment && statSync(dest).isFile() && dest.endsWith('.md')) {
      if (!anchors(dest).has(decodeURIComponent(fragment).toLowerCase())) {
        problems.push(`${file}: no heading #${fragment} in ${relative(root, dest)}`)
      }
    }
  }
}

if (problems.length > 0) {
  console.error(problems.join('\n'))
  process.exit(1)
}
console.log(`docs links ok (${files.length} files)`)
