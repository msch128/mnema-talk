#!/usr/bin/env node
// Fails when a tracked text file contains German outside the allowlist. The
// code, comments, docs and fixtures are English only; German lives in the
// locale resources (UI strings, mail and push texts, seed names) and in tests
// that assert that German output.
//
// A line is German when it has an umlaut or ss-sharp (ä ö ü Ä Ö Ü ß) or one
// of the German words below. The allowlist, scripts/english-only-allowlist.txt,
// has two kinds of entries, each with a reason after " | ":
//   path: <glob> | <reason>    skip the whole file (* matches within a folder, ** across folders)
//   text: <regex> | <reason>   remove matching text first (a person's name, say)
// Usage: node scripts/check-english-only.mjs [--list]
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const allowlistPath = 'scripts/english-only-allowlist.txt'
const LIMIT = Number(process.env.ENGLISH_ONLY_LIMIT || 200)

// Words that are German and not English. Short words that are also English
// (die, der, was, war, an, in, so) are left out on purpose.
const GERMAN_WORDS = [
  'und', 'oder', 'nicht', 'ist', 'wird', 'für', 'mit', 'auf', 'bei', 'noch', 'schon', 'eine', 'einen', 'einem',
  'werden', 'kann', 'aber', 'wenn', 'dass', 'auch', 'sind', 'Datei', 'Konto', 'Hallo', 'hallo', 'bitte', 'wurde', 'diese',
  'dieser', 'keine', 'ohne', 'über', 'nach', 'Kanal', 'Kanäle', 'Nachricht', 'Nachrichten', 'Benutzer',
  'Einstellungen', 'Fehler', 'Deutschland', 'Passwort', 'Rolle', 'Rollen', 'Einladung', 'allgemein', 'Allgemein',
  'zusammen', 'Auszeit', 'Willkommen', 'Löschen', 'Speichern', 'Abbrechen', 'Schließen', 'Bearbeiten',
]
const umlaut = /[äöüÄÖÜß]/
const word = new RegExp(`(?<![\\p{L}\\p{N}_])(${GERMAN_WORDS.join('|')})(?![\\p{L}\\p{N}_])`, 'u')
const skipExt = /\.(png|jpe?g|gif|webp|ico|svg|wasm|woff2?|ttf|otf|mp3|ogg|wav|webm|mp4|pdf|zip|gz|lock|sum|onnx|bin|tgz|icns)$/i

export function parseAllowlist(text) {
  const paths = []
  const texts = []
  text.split(/\r?\n/).forEach((raw, i) => {
    const line = raw.trim()
    if (!line || line.startsWith('#')) return
    const m = /^(path|text):\s*(.+?)\s+\|\s+(\S.*)$/.exec(line)
    if (!m) throw new Error(`${allowlistPath}:${i + 1}: expected "path: <glob> | <reason>" or "text: <regex> | <reason>"`)
    if (m[1] === 'path') paths.push(globToRegExp(m[2]))
    else texts.push(new RegExp(m[2], 'gu'))
  })
  return { paths, texts }
}

function globToRegExp(glob) {
  const body = glob
    .replace(/[.+^${}()|[\]\\]/g, '\\$&')
    .replace(/\*\*/g, '\0')
    .replace(/\*/g, '[^/]*')
    .replace(/\0/g, '.*')
  return new RegExp(`^${body}$`)
}

export function germanLines(content, texts = []) {
  const hits = []
  content.split(/\r?\n/).forEach((raw, i) => {
    const line = texts.reduce((l, re) => l.replace(re, ' '), raw)
    if (umlaut.test(line) || word.test(line)) hits.push([i + 1, raw.trim().slice(0, 140)])
  })
  return hits
}

function main() {
  const { paths, texts } = parseAllowlist(readFileSync(join(root, allowlistPath), 'utf8'))
  const tracked = execFileSync('git', ['ls-files', '-z'], { cwd: root, maxBuffer: 64 * 1024 * 1024 })
    .toString('utf8').split('\0').filter(Boolean)
  const problems = []
  const used = new Set()
  for (const file of tracked) {
    if (skipExt.test(file)) continue
    const rule = paths.findIndex((re) => re.test(file))
    if (rule >= 0) { used.add(rule); continue }
    let buf
    try { buf = readFileSync(join(root, file)) } catch { continue }
    if (buf.includes(0)) continue
    for (const [n, text] of germanLines(buf.toString('utf8'), texts)) problems.push(`${file}:${n}: ${text}`)
  }
  const unused = paths.map((_, i) => i).filter((i) => !used.has(i))
  if (unused.length > 0) {
    console.error(`${allowlistPath} has path entries that match no file (remove them): ${unused.map((i) => paths[i].source).join(", ")}`)
    process.exitCode = 1
  }
  if (problems.length > 0) {
    console.error('German text outside the allowlist (the code and docs are English only):\n' + problems.slice(0, LIMIT).join('\n'))
    if (problems.length > LIMIT) console.error(`... and ${problems.length - LIMIT} more`)
    console.error(`\nTranslate it, move it into a locale resource, or add a reasoned entry to ${allowlistPath}.`)
    process.exitCode = 1
  }
  if (!process.exitCode) console.log(`English only: ${tracked.length} tracked files checked, ${paths.length} allowlisted path patterns`)
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()
