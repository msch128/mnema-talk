// Every literal key passed to t() / $t() in the app must exist in both
// locales, so a typo or a forgotten key fails here instead of showing the raw
// key in the UI. Keys built at runtime (t(`nav.${reason}`)) are not covered.
import { describe, it, expect } from 'vitest'
import de from './de.json'
import en from './en.json'

const sources = import.meta.glob(['../**/*.vue', '../**/*.js', '!../**/*.test.js', '!../third_party/**'], {
  query: '?raw',
  import: 'default',
  eager: true
})

// t('a.b'), $t("a.b.c"), i18n.t('a.b') ... with a literal dotted key.
const CALL = /(?<![\w$])\$?t\(\s*(['"`])([A-Za-z][\w-]*(?:\.[\w-]+)+)\1/g

function lookup(tree, key) {
  let node = tree
  for (const part of key.split('.')) {
    if (node == null || typeof node !== 'object') return undefined
    node = node[part]
  }
  return node
}

// A key resolves to a string, or to a plural object of strings.
function exists(tree, key) {
  const v = lookup(tree, key)
  if (typeof v === 'string') return true
  return !!v && typeof v === 'object' && typeof v.other === 'string'
}

function usedKeys() {
  const used = new Map()
  for (const [file, src] of Object.entries(sources)) {
    for (const m of src.matchAll(CALL)) {
      if (!used.has(m[2])) used.set(m[2], file)
    }
  }
  return used
}

describe('i18n keys used in the code', () => {
  const used = usedKeys()

  it('finds the keys', () => {
    expect(used.size).toBeGreaterThan(100)
  })

  it('exist in German and English', () => {
    const missing = []
    for (const [key, file] of used) {
      for (const [name, tree] of [['de', de], ['en', en]]) {
        if (!exists(tree, key)) missing.push(`${name}: ${key} (${file})`)
      }
    }
    expect(missing).toEqual([])
  })
})
