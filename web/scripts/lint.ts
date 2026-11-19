// Use one ESLint policy for frontend and browser tests, on every host platform.
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { cssFiles, cssLimitViolations } from './cssLimits.ts'

const root = fileURLToPath(new URL('../../', import.meta.url))
const eslint = fileURLToPath(new URL('../node_modules/eslint/bin/eslint.js', import.meta.url))
const result = spawnSync(process.execPath, [eslint, '--config', 'web/eslint.config.ts', 'web', 'e2e'], {
  cwd: root,
  stdio: 'inherit'
})
if (result.error) console.error(result.error.message)
const tooLong = cssLimitViolations(cssFiles(fileURLToPath(new URL('../src', import.meta.url))))
for (const message of tooLong) console.error(message)
process.exitCode = result.status || (tooLong.length > 0 ? 1 : 0)
