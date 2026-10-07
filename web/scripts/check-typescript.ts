// Enforce the complete migration: vue-tsc deliberately skips JavaScript SFCs.
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { join, relative, resolve } from 'node:path'
import { parse } from '@vue/compiler-sfc'

const webRoot = fileURLToPath(new URL('..', import.meta.url))
const repoRoot = resolve(webRoot, '..')
const generatedJavaScript = new Set([
  'web/src/third_party/deepfilternet3/worklet.js',
  'web/src/third_party/deepfilternet3/worker-glue.js'
])
const ignoredDirectories = new Set(['node_modules', 'dist', 'coverage', '.git', '.codex'])
const violations: string[] = []

function inspect(directory: string): void {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const filename = join(directory, entry.name)
    if (entry.isDirectory()) {
      if (!ignoredDirectories.has(entry.name)) inspect(filename)
      continue
    }
    if (!entry.isFile()) continue
    const path = relative(repoRoot, filename).replaceAll('\\', '/')
    if (/\.(?:js|mjs|cjs|jsx)$/.test(entry.name) && !generatedJavaScript.has(path)) violations.push(`${path}: first-party source must be TypeScript`)
    if (entry.name.endsWith('.vue')) {
      const { descriptor, errors } = parse(readFileSync(filename, 'utf8'), { filename })
      if (errors.length) violations.push(`${path}: invalid Vue SFC`)
      for (const script of [descriptor.script, descriptor.scriptSetup]) {
        if (script && script.lang !== 'ts') violations.push(`${path}: every script block must use lang="ts"`)
      }
    }
  }
}
inspect(webRoot)
if (process.argv.includes('--e2e')) inspect(join(repoRoot, 'e2e'))
if (violations.length) {
  for (const violation of violations) console.error(violation)
  console.error(`TypeScript migration incomplete: ${violations.length} source violations`)
  process.exitCode = 1
} else {
  console.log('All checked first-party frontend sources and build tools use TypeScript')
}
