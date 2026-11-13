import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Locale } from './matrix'

// The app's own strings, so the sweep finds buttons by the name a reader sees in either language.

type Tree = { [key: string]: string | Tree }

const load = (locale: Locale): Tree => JSON.parse(readFileSync(fileURLToPath(new URL(`../../../web/src/i18n/${locale}.json`, import.meta.url)), 'utf8')) as Tree
const trees: Record<Locale, Tree> = { en: load('en'), de: load('de') }

export function translate(locale: Locale, key: string, params: Record<string, string> = {}): string {
  let node: string | Tree | undefined = trees[locale]
  for (const part of key.split('.')) node = typeof node === 'object' ? node[part] : undefined
  if (typeof node !== 'string') throw new Error(`no ${locale} string for ${key}`)
  return node.replace(/\{(\w+)\}/g, (whole, name: string) => params[name] ?? whole)
}
