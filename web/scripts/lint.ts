// Use one ESLint policy for frontend and browser tests, on every host platform.
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../../', import.meta.url))
const eslint = fileURLToPath(new URL('../node_modules/eslint/bin/eslint.js', import.meta.url))
const result = spawnSync(process.execPath, [eslint, '--config', 'web/eslint.config.ts', 'web', 'e2e'], {
  cwd: root,
  stdio: 'inherit'
})
if (result.error) console.error(result.error.message)
process.exitCode = result.status ?? 1
