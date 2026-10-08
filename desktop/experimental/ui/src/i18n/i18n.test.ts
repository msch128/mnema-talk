import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createApp } from 'vue'
import { t, setLocale, locale, detectLocale, registerMessages, flattenKeys, chooseLocale, explicitLocale, browserLocale, i18nPlugin } from './index'
import de from './de.json'
import en from './en.json'

describe('i18n', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); setLocale('de') })
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

  it('remembers an explicit language choice and ignores an unsupported replacement', () => {
    chooseLocale('en')
    expect(explicitLocale()).toBe('en')
    expect(locale.value).toBe('en')
    chooseLocale('fr')
    expect(explicitLocale()).toBe('en')
    chooseLocale('de')
    expect(explicitLocale()).toBe('de')
  })

  it('uses German without browser globals and can change language without a DOM', () => {
    vi.stubGlobal('navigator', { language: 'EN-gb' })
    expect(browserLocale()).toBe('en')
    vi.stubGlobal('navigator', undefined)
    expect(browserLocale()).toBe('de')
    vi.stubGlobal('document', undefined)
    setLocale('en')
    expect(locale.value).toBe('en')
  })

  it('preserves the selected locale if the browser refuses the html language update', () => {
    vi.stubGlobal('document', { get documentElement() { throw new Error('synthetic unavailable DOM') } })
    expect(() => setLocale('en')).not.toThrow()
    expect(locale.value).toBe('en')
  })

  it('installs reactive translations on a Vue application', () => {
    const app = createApp({ render: () => null })
    app.use(i18nPlugin)
    expect(app.config.globalProperties.$t('__t.hello', { name: 'Zoe' })).toBe('Hallo Zoe')
    expect(app.config.globalProperties.$i18nLocale).toBe(locale)
    chooseLocale('en')
    expect(app.config.globalProperties.$t('__t.hello', { name: 'Zoe' })).toBe('Hello Zoe')
  })

  it('handles missing and invalid plural counts and retains unresolved placeholders', () => {
    expect(t('__t.items')).toBe('{count} Einträge')
    expect(t('__t.items', { count: 'not-a-number' })).toBe('not-a-number Einträge')
    expect(t('__t.hello', { name: null })).toBe('Hallo {name}')
    expect(t('__t.hello.child')).toBe('__t.hello.child')
    registerMessages('de', { __fallback: { items: { one: 'single' }, nested: { only: { deep: 'leaf' } } } })
    expect(t('__fallback.items', { count: 2 })).toBe('__fallback.items')
    expect(t('__fallback.nested', { count: 1 })).toBe('__fallback.nested')
  })

  it('falls back to basic one/other selection when Intl plural rules are unavailable', () => {
    vi.spyOn(Intl, 'PluralRules').mockImplementation(() => { throw new Error('synthetic unsupported locale') })
    expect(t('__t.items', { count: 1 })).toBe('1 Eintrag')
    expect(t('__t.items', { count: 2 })).toBe('2 Einträge')
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
    const ph = (s: unknown) => [...String(s).matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort().join(',')
    for (const [k, v] of Object.entries(fd)) {
      expect(ph(fe[k]), k).toBe(ph(v))
    }
  })
})
