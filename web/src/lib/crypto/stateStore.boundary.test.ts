// @vitest-environment node
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

// This file sits in web/src/lib/crypto, so web/src is two directories up.
const webSrc = join(dirname(fileURLToPath(import.meta.url)), '..', '..')
// Split so this file's own text is not an import of the store module.
const storeModule = ['state', 'Store'].join('')

const allowedImporters = new Set([
  'lib/crypto/stateStore.ts',
  'lib/crypto/stateStoreTypes.ts',
  'lib/crypto/stateStore.test.ts',
])

const sourceFile = /\.(?:vue|[cm]?[jt]sx?)$/

function posixRelative(file: string): string {
  return relative(webSrc, file).split(sep).join('/')
}

function listSources(dir: string): string[] {
  const found: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name)
    if (entry.isDirectory()) found.push(...listSources(full))
    else if (entry.isFile() && sourceFile.test(entry.name)) found.push(full)
  }
  return found
}

function namesStore(specifier: string): boolean {
  const path = specifier.split(/[?#]/, 1)[0] ?? ''
  const segment = path.split('/').pop() ?? ''
  const bare = segment.replace(/(?:\.(?:d\.ts|tsx|ts|jsx|js|mjs|cjs))+$/, '')
  return bare === storeModule
}

function storeSpecifiers(source: string): string[] {
  const specifierPattern = /(?:\bfrom\s*|\bimport\s*\(\s*|\brequire\s*\(\s*|\bimport\s+)['"]([^'"]+)['"]/g
  const hits: string[] = []
  for (const match of source.matchAll(specifierPattern)) {
    const specifier = match[1]
    if (specifier !== undefined && namesStore(specifier)) hits.push(specifier)
  }
  return hits
}

function filesImportingStore(): string[] {
  return listSources(webSrc)
    .filter(file => storeSpecifiers(readFileSync(file, 'utf8')).length > 0)
    .map(posixRelative)
    .sort()
}

describe('crypto state store import boundary', () => {
  it('is imported under web/src only by its own module files', () => {
    const outside = filesImportingStore().filter(file => !allowedImporters.has(file))
    expect(outside, 'no product file may import the crypto state store').toEqual([])
  })

  it('still sees the module test, so an empty detector cannot pass', () => {
    expect(filesImportingStore()).toContain('lib/crypto/stateStore.test.ts')
  })

  it('treats a component import of the store as a boundary break', () => {
    const line = "import { openCryptoStateStore } from './" + storeModule + "'"
    expect(storeSpecifiers(line)).toEqual(['./' + storeModule])
    const typesOnly = "export type { CryptoStateStore } from './" + storeModule + "Types'"
    expect(storeSpecifiers(typesOnly)).toEqual([])
  })
})
