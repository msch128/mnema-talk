// Build-time version of the web app (vite define __APP_VERSION__, see
// src/lib/appVersion.js). It must match the version linked into the Go
// binary, so both read the same source: MNEMA_VERSION (Makefile, Dockerfile,
// release workflow), else ../version.txt.
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const SAFE = /^[0-9A-Za-z.+_-]{1,64}$/

function readVersionTxt() {
  return readFileSync(fileURLToPath(new URL('../version.txt', import.meta.url)), 'utf8')
}

export function appVersion(env = process.env, readVersionFile = readVersionTxt) {
  let v = (env.MNEMA_VERSION || '').trim()
  if (!v) {
    try {
      v = readVersionFile().trim()
    } catch {
      v = ''
    }
  }
  v = v.replace(/^v/, '')
  return SAFE.test(v) ? v : 'dev'
}
