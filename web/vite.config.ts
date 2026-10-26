import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'
import { readdirSync, readFileSync, writeFileSync, mkdirSync, copyFileSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { brotliCompressSync, gzipSync, constants as zlib } from 'node:zlib'
import { appVersion } from './version.config.ts'
import { serviceWorker } from './scripts/serviceWorkerBuild.ts'
import { emojiPickerCsp } from './scripts/emojiPickerCsp.ts'

// Writes .br and .gz next to compressible build assets; web.Handler serves
// them to browsers that accept the encoding. Matters most for the 16 MB
// DeepFilterNet wasm (2.7 MB as Brotli). Already compressed formats (fonts,
// the .tgz model) are left alone.
function precompress(): Plugin {
  const compressible = /\.(js|css|wasm|svg|json)$/
  return {
    name: 'mnema-precompress',
    apply: 'build',
    closeBundle() {
      const dir = fileURLToPath(new URL('./dist/assets', import.meta.url))
      if (!existsSync(dir)) return
      for (const name of readdirSync(dir)) {
        if (!compressible.test(name)) continue
        const file = join(dir, name)
        const data = readFileSync(file)
        if (data.length < 1024) continue
        writeFileSync(`${file}.br`, brotliCompressSync(data, { params: { [zlib.BROTLI_PARAM_QUALITY]: 11 } }))
        writeFileSync(`${file}.gz`, gzipSync(data, { level: 9 }))
      }
    }
  }
}

// License and NOTICE texts of bundled packages whose builds drop their
// license comments; shipped under /licenses/ (see THIRD_PARTY_NOTICES.md).
const SHIPPED_LICENSES = {
  'tauri-api/LICENSE-MIT': '@tauri-apps/api/LICENSE-MIT',
  'tauri-api/LICENSE-APACHE-2.0': '@tauri-apps/api/LICENSE-APACHE-2.0',
  'swagger-ui/LICENSE': 'swagger-ui-dist/LICENSE',
  'swagger-ui/NOTICE': 'swagger-ui-dist/NOTICE',
  'swagger-ui/bundled-components.txt': 'swagger-ui-dist/swagger-ui-es-bundle.js.LICENSE.txt',
  'highlight.js/LICENSE': 'highlight.js/LICENSE',
  'emoji-picker-element/LICENSE': 'emoji-picker-element/LICENSE',
  'emoji-picker-element-data/LICENSE': 'emoji-picker-element-data/LICENSE'
}

function shipLicenses(): Plugin {
  return {
    name: 'mnema-ship-licenses',
    apply: 'build',
    closeBundle() {
      for (const [to, from] of Object.entries(SHIPPED_LICENSES)) {
        const dest = fileURLToPath(new URL(`./dist/licenses/${to}`, import.meta.url))
        mkdirSync(dirname(dest), { recursive: true })
        copyFileSync(fileURLToPath(new URL(`./node_modules/${from}`, import.meta.url)), dest)
      }
    }
  }
}

export default defineConfig(({ mode }) => ({
  plugins: [emojiPickerCsp(), vue(), tailwindcss(), precompress(), shipLicenses(), serviceWorker()],
  define: {
    __APP_VERSION__: JSON.stringify(appVersion()),
    // Test-only timing hook for the media key lifetimes (lib/media/bounds.ts):
    // on in unit tests and in `MNEMA_E2E=1` builds, never in a release build.
    __MNEMA_E2E_HOOKS__: JSON.stringify(mode === 'test' || process.env.MNEMA_E2E === '1')
  },
  // The dev server must not pre-bundle the picker, or the CSP patch is skipped.
  optimizeDeps: { exclude: ['emoji-picker-element'] },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    port: 3000,
    proxy: {
      // The Go server checks Origin; add http://localhost:3000 to
      // CORS_ALLOWED_ORIGINS when developing through this proxy.
      '/api': {
        target: 'http://localhost:8080',
        ws: true
      }
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // Source maps would publish the original sources from the server.
    sourcemap: false,
    rollupOptions: {
      // api-docs.html is the API reference (Swagger UI), served by the Go
      // server at /api/docs for signed-in members.
      input: {
        main: fileURLToPath(new URL('./index.html', import.meta.url)),
        apiDocs: fileURLToPath(new URL('./api-docs.html', import.meta.url))
      }
    }
  },
  test: {
    // Every test file runs in its own process, not a worker thread: a V8
    // fatal error in one file (a SIGTRAP was seen once in CI) then cannot take
    // a shared worker down mid-run, and native/WASM state never leaks between files.
    pool: 'forks',
    maxWorkers: 4,
    environment: 'happy-dom',
    include: ['src/**/*.test.{js,ts}'],
    setupFiles: ['./src/test-setup.ts'],
    // `npm run test:coverage` (CI): every source file counts, tested or not,
    // so the totals show untested code instead of hiding it.
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{js,ts,vue}'],
      exclude: ['src/**/*.test.{js,ts}', 'src/**/*.fixture.{js,ts}', 'src/test-setup.{js,ts}', 'src/**/*.d.ts', 'src/third_party/**'],
      reporter: ['text-summary', 'json', 'json-summary', 'lcov'],
      reportsDirectory: './coverage'
    }
  }
}))
