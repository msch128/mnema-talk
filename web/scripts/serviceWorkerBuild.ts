// Builds the service worker (src/sw/sw.ts) into dist/sw.js after the app, so it
// can list the app's hashed files for its offline shell. It is built apart from
// the app on purpose: a worker cannot share chunks with the page, and it must
// keep the fixed name /sw.js to control the whole origin.
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { build, type Plugin } from 'vite'

/** Files every install stores besides the hashed ones index.html loads. */
const FIXED_FILES = ['/manifest.webmanifest', '/favicon.svg', '/pwa-icon-192.png', '/pwa-icon-512.png', '/pwa-icon-maskable-512.png', '/apple-touch-icon.png']

/** The files of the offline shell: the page, what it loads from /assets/, and the fixed icons and manifest. */
export function shellFiles(indexHtml: string): string[] {
  const loaded = [...indexHtml.matchAll(/(?:src|href)="(\/assets\/[^"]+)"/g)].map(m => m[1] ?? '')
  return ['/', ...new Set(loaded), ...FIXED_FILES]
}

/** Names the cache of one build: the same files always give the same name. */
export function shellVersion(files: readonly string[]): string {
  return createHash('sha256').update(files.join('\n')).digest('hex').slice(0, 12)
}

export function serviceWorker(): Plugin {
  return {
    name: 'mnema-service-worker',
    apply: 'build',
    async closeBundle() {
      const files = shellFiles(readFileSync(fileURLToPath(new URL('../dist/index.html', import.meta.url)), 'utf8'))
      await build({
        configFile: false,
        logLevel: 'warn',
        publicDir: false,
        define: { __SHELL_VERSION__: JSON.stringify(shellVersion(files)), __SHELL_FILES__: JSON.stringify(files) },
        build: {
          outDir: fileURLToPath(new URL('../dist', import.meta.url)),
          emptyOutDir: false,
          sourcemap: false,
          lib: { entry: fileURLToPath(new URL('../src/sw/sw.ts', import.meta.url)), formats: ['iife'], name: 'mnemaServiceWorker', fileName: () => 'sw.js' },
        },
      })
    },
  }
}
