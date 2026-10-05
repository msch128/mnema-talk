import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import { readdirSync, readFileSync, writeFileSync, mkdirSync, copyFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { brotliCompressSync, gzipSync, constants as zlib } from 'node:zlib'

// Writes .br and .gz next to compressible build assets; web.Handler serves
// them to browsers that accept the encoding. Matters most for the 16 MB
// DeepFilterNet wasm (2.7 MB as Brotli). Already compressed formats (fonts,
// the .tgz model) are left alone.
function precompress() {
  const compressible = /\.(js|css|wasm|svg|json)$/
  return {
    name: 'mnema-precompress',
    apply: 'build',
    closeBundle() {
      const dir = fileURLToPath(new URL('./dist/assets', import.meta.url))
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
  'swagger-ui/LICENSE': 'swagger-ui-dist/LICENSE',
  'swagger-ui/NOTICE': 'swagger-ui-dist/NOTICE',
  'swagger-ui/bundled-components.txt': 'swagger-ui-dist/swagger-ui-es-bundle.js.LICENSE.txt',
  'emoji-picker-element/LICENSE': 'emoji-picker-element/LICENSE',
  'emoji-picker-element-data/LICENSE': 'emoji-picker-element-data/LICENSE'
}

function shipLicenses() {
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

export default defineConfig({
  plugins: [vue(), precompress(), shipLicenses()],
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
    environment: 'happy-dom',
    include: ['src/**/*.test.js'],
    setupFiles: ['./src/test-setup.js']
  }
})
