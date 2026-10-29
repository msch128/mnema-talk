#!/usr/bin/env node
// Static onboarding checks: the docs must keep matching the product. No network.
//   F1  command blocks do not pin a release older than the current minor
//   F4  every setup check code the server can send is listed in the docs
//   F5  doc/ports.md lists exactly the inbound ports docker-compose.yml publishes
// Usage: node scripts/check-onboarding-docs.mjs
import { existsSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (p) => readFileSync(join(root, p), 'utf8')

// ---- F1: stale release versions in copy-paste blocks -----------------------

const IMAGE_TAG = /mnema-talk:v?(\d+)\.(\d+)(?:\.(\d+))?(?![\w.-]*\/)/g
const VERSION_VAR = /\b(?:VERSION|NEW|TAG)=v?(\d+)\.(\d+)\.\d+\b/g

function fencedBlocks(text) {
  const lines = text.split('\n')
  const out = []
  for (let i = 0; i < lines.length; i++) {
    const open = /^(\s*)(`{3,}|~{3,})/.exec(lines[i])
    if (!open) continue
    const body = []
    let k = i + 1
    while (k < lines.length && !lines[k].trim().startsWith(open[2])) body.push([k + 1, lines[k++]])
    out.push({ lines: body, allowOld: lines.slice(Math.max(0, i - 2), i).some((l) => /test:allow-old-version/.test(l)) })
    i = k
  }
  return out
}

const older = (maj, min, cur) => Number(maj) < cur.major || (Number(maj) === cur.major && Number(min) < cur.minor)

export function staleVersions(file, text, cur) {
  const problems = []
  const scan = (lineNo, line) => {
    for (const re of [IMAGE_TAG, VERSION_VAR]) {
      for (const m of line.matchAll(re)) {
        if (older(m[1], m[2], cur)) {
          problems.push(`${file}:${lineNo}: "${m[0]}" is older than the current release ${cur.text}; use :latest, :${cur.major}.${cur.minor} or compute the version (mark a deliberate old example with <!-- test:allow-old-version --> above its fence)`)
        }
      }
    }
  }
  if (!file.endsWith('.md')) {
    text.split('\n').forEach((l, i) => scan(i + 1, l))
    return problems
  }
  for (const b of fencedBlocks(text)) {
    if (b.allowOld) continue
    for (const [n, l] of b.lines) scan(n, l)
  }
  return problems
}

function checkVersions() {
  const [major, minor] = read('version.txt').trim().split('.').map(Number)
  const cur = { major, minor, text: read('version.txt').trim() }
  const files = ['README.md', 'docker-compose.yml', '.env.example', ...readdirSync(join(root, 'doc')).filter((f) => f.endsWith('.md')).map((f) => `doc/${f}`)]
  return files.flatMap((f) => staleVersions(f, read(f), cur))
}

// ---- F4: setup check codes in the docs -------------------------------------

export function missingCodes(codes, docs) {
  return codes.filter((c) => !docs.some((d) => d.includes('`' + c + '`'))).map(
    (c) => `setup check code "${c}" is not in a docs table: add a row \`${c}\` to "Setup checks" in doc/operations.md (or doc/troubleshooting-voice.md)`,
  )
}

function checkCodes() {
  const codes = JSON.parse(read('api/status-codes.json')).setup_checks
  const docs = ['doc/operations.md', 'doc/troubleshooting-voice.md'].filter((f) => existsSync(join(root, f))).map(read)
  return missingCodes(codes, docs)
}

// ---- F5: published ports vs doc/ports.md -----------------------------------

const subst = (s) => s.replace(/\$\{[A-Z_0-9]+:-([^}]*)\}/g, '$1')

// composePorts returns Map "port/proto" -> where it comes from.
export function composePorts(compose) {
  const out = new Map()
  const add = (spec, proto, src) => out.set(`${spec}/${proto}`, src)
  const lines = compose.split('\n')
  for (let i = 0; i < lines.length; i++) {
    if (!/^\s+ports:\s*$/.test(lines[i])) continue
    for (let k = i + 1; k < lines.length; k++) {
      const m = /^\s+- ["']?([^"'\s#]+)["']?\s*(?:#.*)?$/.exec(lines[k])
      if (!m) {
        if (/^\s*#/.test(lines[k]) || lines[k].trim() === '') continue
        break
      }
      const parts = subst(m[1]).split('/')
      const proto = parts[1] ?? 'tcp'
      const hostPart = parts[0].split(':').slice(-2)[0] // [addr:]host:container
      add(hostPart, proto, `docker-compose.yml line ${k + 1}`)
    }
  }
  // coturn runs with host networking: its ports are command line flags.
  const listen = /--listening-port=(\d+)/.exec(compose)
  if (listen) {
    add(listen[1], 'udp', 'the coturn service')
    add(listen[1], 'tcp', 'the coturn service')
  }
  const range = /--min-port=(\d+)\s+--max-port=(\d+)/.exec(compose)
  if (range) add(`${range[1]}-${range[2]}`, 'udp', 'the coturn service')
  return out
}

// docPorts reads the "Inbound ports" table; rows served by the reverse proxy
// (443, 80) are not published by compose and are skipped.
export function docPorts(md) {
  const start = md.indexOf('## Inbound ports')
  if (start < 0) throw new Error('doc/ports.md has no "## Inbound ports" section')
  const section = md.slice(start).split(/\n## /)[0]
  const out = new Map()
  for (const row of section.split('\n')) {
    const c = row.split('|').map((x) => x.trim())
    if (c.length < 5 || !/^\d/.test(c[1])) continue
    if (/^your reverse proxy/i.test(c[4])) continue
    const protos = /tcp/i.test(c[2]) && /udp/i.test(c[2]) ? ['tcp', 'udp'] : [c[2].toLowerCase()]
    for (const p of protos) out.set(`${c[1]}/${p}`, `doc/ports.md row "${c[1]} ${c[2]}"`)
  }
  return out
}

export function comparePorts(fromCompose, fromDoc) {
  const problems = []
  for (const [k, src] of fromCompose) {
    if (!fromDoc.has(k)) problems.push(`port ${k} is published by ${src} but missing from the "Inbound ports" table in doc/ports.md`)
  }
  for (const [k, src] of fromDoc) {
    if (!fromCompose.has(k)) problems.push(`port ${k} is listed in ${src} but docker-compose.yml does not publish it`)
  }
  return problems
}

function checkPorts() {
  return comparePorts(composePorts(read('docker-compose.yml')), docPorts(read('doc/ports.md')))
}

// ---- main ------------------------------------------------------------------

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const problems = [...checkVersions(), ...checkCodes(), ...checkPorts()]
  if (problems.length > 0) {
    console.error(problems.map((p) => `onboarding docs: ${p}`).join('\n'))
    process.exit(1)
  }
  console.log('onboarding docs check passed (versions, setup check codes, ports)')
}
