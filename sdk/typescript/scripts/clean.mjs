// Removes dist/ before a build so that deleted sources leave no stale output.
import { rmSync } from 'node:fs'

rmSync(new URL('../dist', import.meta.url), { recursive: true, force: true })
