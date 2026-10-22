// Entry point of the service worker, built on its own into dist/sw.js (see
// the mnema-service-worker plugin in vite.config.ts) and served from the root
// so its scope covers the whole app. The build fills in the two constants.
import { installWorker, type WorkerScope } from './worker.ts'

declare const __SHELL_VERSION__: string
declare const __SHELL_FILES__: readonly string[]

installWorker(self as unknown as WorkerScope, { version: __SHELL_VERSION__, precache: __SHELL_FILES__ })
