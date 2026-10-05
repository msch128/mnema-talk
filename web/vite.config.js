import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
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

export default defineConfig({
  plugins: [vue(), precompress()],
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
    sourcemap: false
  },
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.test.js'],
    setupFiles: ['./src/test-setup.js']
  }
})
