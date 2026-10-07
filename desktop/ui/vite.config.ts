import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const licenseFile = fileURLToPath(new URL('../THIRD_PARTY_NOTICES.md', import.meta.url))

export default defineConfig({
  plugins: [vue()],
  clearScreen: false,
  server: {
    host: '127.0.0.1',
    port: 1420,
    strictPort: true,
    forwardConsole: false,
    // Vite checks both the physical file and the raw-query module ID.
    fs: { allow: [fileURLToPath(new URL('.', import.meta.url)), licenseFile, `${licenseFile}?raw`] }
  },
  build: { sourcemap: false, target: 'es2022' },
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.ts', 'src/**/*.vue'],
      exclude: ['src/**/*.test.ts', 'src/main.ts'],
      reporter: ['text', 'json-summary', 'html'],
      thresholds: { statements: 95, branches: 95, functions: 95, lines: 95 }
    }
  }
})
