// Style sheets obey the same file-size limit as the code (ESLint max-lines covers only ts and vue).
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

export const MAX_CSS_LINES = 400

/** Every .css file under dir, skipping vendored code and build output. */
export function cssFiles(dir: string): string[] {
  const found: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === 'third_party' || entry.name === 'node_modules' || entry.name === 'dist') continue
    const path = join(dir, entry.name)
    if (entry.isDirectory()) found.push(...cssFiles(path))
    else if (entry.name.endsWith('.css')) found.push(path)
  }
  return found
}

/** One message per file longer than the limit. */
export function cssLimitViolations(files: readonly string[], read: (file: string) => string = (f) => readFileSync(f, 'utf8')): string[] {
  const out: string[] = []
  for (const file of files) {
    const lines = read(file).split('\n').length - 1
    if (lines > MAX_CSS_LINES) out.push(`${file}: ${lines} lines, the limit is ${MAX_CSS_LINES}`)
  }
  return out
}
