// Guards the i18n rule: no user-visible text is hard-coded in a component.
// A heuristic scan of every .vue file: static text nodes, static title/aria-label/
// placeholder/alt attributes, and German-looking string literals in scripts.
import { describe, it, expect } from 'vitest'

const vueFiles = import.meta.glob('../**/*.vue', { query: '?raw', import: 'default', eager: true })
const jsFiles = import.meta.glob(['../**/*.js', '!../**/*.test.js', '!../i18n/**'], { query: '?raw', import: 'default', eager: true })

const UMLAUT = /[äöüÄÖÜß]/
// Common German function words / UI words that would signal hard-coded German.
const GERMAN_WORDS = /\b(und|oder|nicht|der|die|das|ein|eine|für|mit|von|zu|wird|werden|Nachricht|Kanal|Kanäle|Antwort|Antworten|Abbrechen|Speichern|Löschen|Schließen|Senden|Mitglieder|Verbunden|Benutzername|Passwort)\b/
// Words that are fine as-is (brand, units, key names, symbols).
const ALLOWED_TEXT = /^(Mnema Talk|M|ms|MB|kbit\/s|Ø|Esc|Enter|Admin|Opus|WebRTC|min|max|%|DTLS-SRTP)$/

function templateOf(src) {
  const m = src.match(/<template>([\s\S]*)<\/template>\s*(?:<style|$)/)
  return m ? m[1] : ''
}

// Removes HTML comments until none are left, so nested fragments like
// `<!<!---->--` cannot leave a new comment behind.
function stripComments(src) {
  let prev
  do {
    prev = src
    src = src.replace(/<!--[\s\S]*?-->/g, '')
  } while (src !== prev)
  return src.replace(/<!--/g, '')
}

function staticText(template) {
  const t = stripComments(template)
    .replace(/\{\{[\s\S]*?\}\}/g, ' ')
  const out = []
  // text between tags
  for (const m of t.matchAll(/>([^<>]+)</g)) out.push(m[1].replace(/&[a-z]+;/g, ' ').trim())
  return out.filter(x => /[A-Za-zÄÖÜäöüß]{2,}/.test(x) && !ALLOWED_TEXT.test(x))
}

function staticAttrs(template) {
  const out = []
  for (const m of template.matchAll(/\s(title|aria-label|placeholder|alt)="([^"]*)"/g)) {
    // `:title="…"` is dynamic: the regex requires whitespace before the bare name.
    if (/[A-Za-zÄÖÜäöüß]{2,}/.test(m[2])) out.push(`${m[1]}="${m[2]}"`)
  }
  return out
}

describe('scanner self-check', () => {
  it('flags static text and attributes but not bound ones', () => {
    expect(staticText('<span>Speichern</span><span>{{ $t(\'x\') }}</span>')).toEqual(['Speichern'])
    expect(staticAttrs('<button title="Schließen" :aria-label="$t(\'x\')"></button>')).toEqual(['title="Schließen"'])
  })
})

describe('no hard-coded UI text', () => {
  for (const [path, src] of Object.entries(vueFiles)) {
    const name = path.replace(/^\.\.\//, '')
    it(`${name} template has no static text`, () => {
      const tpl = templateOf(src)
      expect(staticText(tpl), 'static text nodes').toEqual([])
      expect(staticAttrs(tpl), 'static title/aria-label/placeholder/alt').toEqual([])
    })

    it(`${name} has no German string literals`, () => {
      const script = (src.match(/<script\b[^>]*>([\s\S]*?)<\/script\b[^>]*>/i)?.[1] ?? '')
      const code = script.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/.*$/gm, '$1')
      const literals = [...code.matchAll(/'([^'\n]*)'|"([^"\n]*)"|`([^`]*)`/g)].map(m => m[1] ?? m[2] ?? m[3])
      expect(literals.filter(l => UMLAUT.test(l) || GERMAN_WORDS.test(l))).toEqual([])
      const tpl = stripComments(templateOf(src))
      expect(tpl.match(UMLAUT) ? tpl.split('\n').filter(l => UMLAUT.test(l)) : []).toEqual([])
    })
  }

  for (const [path, src] of Object.entries(jsFiles)) {
    const name = path.replace(/^\.\.\//, '')
    it(`${name} has no German string literals`, () => {
      const code = src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/.*$/gm, '$1')
      const literals = [...code.matchAll(/'([^'\n]*)'|"([^"\n]*)"|`([^`]*)`/g)].map(m => m[1] ?? m[2] ?? m[3])
      expect(literals.filter(l => UMLAUT.test(l))).toEqual([])
    })
  }
})
