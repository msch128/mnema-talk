import { describe, it, expect, beforeEach } from 'vitest'
import { t, setLocale, locale, detectLocale, registerMessages, flattenKeys } from './index'
import de from './de.json'
import en from './en.json'

describe('i18n', () => {
  beforeEach(() => {
    setLocale('de')
    registerMessages('de', { __t: { hello: 'Hallo {name}', items: { one: '{count} Eintrag', other: '{count} Einträge' }, onlyDe: 'nur deutsch' } })
    registerMessages('en', { __t: { hello: 'Hello {name}', items: { one: '{count} item', other: '{count} items' } } })
  })

  it('interpolates params', () => {
    expect(t('__t.hello', { name: 'Zoe' })).toBe('Hallo Zoe')
    setLocale('en')
    expect(t('__t.hello', { name: 'Zoe' })).toBe('Hello Zoe')
  })

  it('keeps unknown placeholders visible', () => {
    expect(t('__t.hello')).toBe('Hallo {name}')
  })

  it('selects plural forms by count', () => {
    expect(t('__t.items', { count: 1 })).toBe('1 Eintrag')
    expect(t('__t.items', { count: 3 })).toBe('3 Einträge')
    expect(t('__t.items', { count: 0 })).toBe('0 Einträge')
    setLocale('en')
    expect(t('__t.items', { count: 1 })).toBe('1 item')
    expect(t('__t.items', { count: 2 })).toBe('2 items')
  })

  it('falls back to German, then to the key', () => {
    setLocale('en')
    expect(t('__t.onlyDe')).toBe('nur deutsch')
    expect(t('__t.nope')).toBe('__t.nope')
  })

  it('ignores unsupported locales', () => {
    setLocale('fr')
    expect(locale.value).toBe('de')
  })

  it('sets the html lang attribute', () => {
    setLocale('en')
    expect(document.documentElement.lang).toBe('en')
  })

  it('detects the browser language', () => {
    expect(detectLocale('en-US')).toBe('en')
    expect(detectLocale('en')).toBe('en')
    expect(detectLocale('de-AT')).toBe('de')
    expect(detectLocale('fr-FR')).toBe('de')
    expect(detectLocale(undefined)).toBe('de')
  })
})

describe('locale files', () => {
  it('de and en have exactly the same keys', () => {
    const d = flattenKeys(de).sort()
    const e = flattenKeys(en).sort()
    expect(e.filter(k => !d.includes(k))).toEqual([])
    expect(d.filter(k => !e.includes(k))).toEqual([])
  })

  it('every placeholder in de also appears in en', () => {
    const fd = flattenKeys(de, true)
    const fe = flattenKeys(en, true)
    const ph = s => [...String(s).matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort().join(',')
    for (const [k, v] of Object.entries(fd)) {
      expect(ph(fe[k]), k).toBe(ph(v))
    }
  })
})
