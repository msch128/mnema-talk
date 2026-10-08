// Tiny dependency-free i18n. German is the default and the fallback; English
// is the second language. Messages live in de.json / en.json as nested
// objects addressed with dotted keys ("chat.send"). A leaf is a string with
// {name} placeholders, or a plural object { one, other } picked by
// params.count.
import { ref } from 'vue'
import type { App } from 'vue'

export type Locale = 'de' | 'en'
export type TranslationParams = Readonly<Record<string, string | number | boolean | null | undefined>>
export interface MessageTree { [key: string]: string | MessageTree }
type MessageValue = string | MessageTree
import de from './de.json'
import en from './en.json'

export const SUPPORTED = ['de', 'en'] as const
export const DEFAULT_LOCALE: Locale = 'de'

const messages: Record<Locale, MessageTree> = { de, en }

export const locale = ref<Locale>(DEFAULT_LOCALE)

function applyHtmlLang(l: Locale) {
  try {
    if (typeof document !== 'undefined') document.documentElement.lang = l
  } catch {
    // no DOM
  }
}

export function isSupportedLocale(value: unknown): value is Locale {
  return value === 'de' || value === 'en'
}

/** Switches the UI language; unknown locales are ignored. */
export function setLocale(l: unknown): void {
  if (!isSupportedLocale(l)) return
  locale.value = l
  applyHtmlLang(l)
}

let explicitChoice: Locale | null = null

/** A language picked by the user on purpose (e.g. on the login screen). */
export function chooseLocale(l: unknown): void {
  if (!isSupportedLocale(l)) return
  explicitChoice = l
  setLocale(l)
}

export function explicitLocale(): Locale | null {
  return explicitChoice
}

/** The browser rule: English if the browser says so, otherwise German. */
export function detectLocale(navLang: unknown): Locale {
  return typeof navLang === 'string' && navLang.toLowerCase().startsWith('en') ? 'en' : 'de'
}

export function browserLocale(): Locale {
  return detectLocale(typeof navigator !== 'undefined' ? navigator.language : '')
}

/** Adds (or overrides) messages for a locale. Used by tests and lazy modules. */
export function registerMessages(l: Locale, extra: MessageTree): void {
  messages[l] = { ...(messages[l] || {}), ...extra }
}

function lookup(tree: MessageTree, key: string): MessageValue | undefined {
  let node: MessageValue | undefined = tree
  for (const part of key.split('.')) {
    if (node == null || typeof node !== 'object') return undefined
    node = node[part]
  }
  return node
}

function interpolate(str: string, params: TranslationParams | undefined): string {
  return str.replace(/\{(\w+)\}/g, (m: string, name: string) => (params && params[name] != null ? String(params[name]) : m))
}

function pluralForm(l: Locale, count: number): Intl.LDMLPluralRule {
  try {
    return new Intl.PluralRules(l).select(count)
  } catch {
    return count === 1 ? 'one' : 'other'
  }
}

function resolve(l: Locale, key: string, params: TranslationParams | undefined): string | undefined {
  let v = lookup(messages[l], key)
  if (v == null) return undefined
  if (typeof v === 'object') {
    const n = Number(params?.count)
    v = v[pluralForm(l, Number.isFinite(n) ? n : 0)] ?? v.other
    if (typeof v !== 'string') return undefined
  }
  return typeof v === 'string' ? interpolate(v, params) : undefined
}

/** Translates a key. Falls back to German, then to the key itself. */
export function t(key: string, params?: TranslationParams): string {
  const current = locale.value // reactive read: templates re-render on switch
  return resolve(current, key, params) ?? resolve(DEFAULT_LOCALE, key, params) ?? key
}

/** Vue plugin: gives templates a global $t and $locale. */
export const i18nPlugin = {
  install(app: App) {
    app.config.globalProperties.$t = t
    app.config.globalProperties.$i18nLocale = locale
  }
}

/** Dotted leaf keys of a message tree; plural objects count as one leaf each
 *  unless `asMap` is set, which returns { key: value } instead. */
export function flattenKeys(tree: MessageTree, asMap?: false, prefix?: string): string[]
export function flattenKeys(tree: MessageTree, asMap: true, prefix?: string): Record<string, string>
export function flattenKeys(tree: MessageTree, asMap = false, prefix = ''): string[] | Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !isPluralObject(v)) {
      Object.assign(out, flattenKeys(v, true, path))
    } else if (isPluralObject(v)) {
      for (const [form, s] of Object.entries(v)) out[`${path}.${form}`] = s
    } else {
      out[path] = v
    }
  }
  return asMap ? out : Object.keys(out)
}

function isPluralObject(v: MessageValue): v is Record<string, string> {
  return v != null && typeof v === 'object' && 'other' in v && Object.values(v).every(x => typeof x === 'string')
}
