// The reaction form is one rule in two languages: this file and the Go test
// TestReactionFormsMatchTheVectors (internal/chat/core/reaction_form_test.go)
// both check their implementation against
// internal/chat/core/testdata/reaction_forms.json. After changing the rule, the
// cases below or the picker's data:
//   REACTION_FORMS_WRITE=1 npx vitest run src/shared/lib/reactionForm.test.ts
// and run the Go test, which must agree without being regenerated.
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { isTextDefaultEmoji, reactionForm, reactionKey } from './reactionForm'

const root = join(import.meta.dirname, '..', '..', '..', '..')
const file = join(root, 'internal/chat/core/testdata/reaction_forms.json')
interface Vector { emoji: string; form: string }

/** Every emoji the full picker offers, with every skin tone, as it hands them over. */
function pickerEmoji(): string[] {
  const data = JSON.parse(readFileSync(join(root, 'web/node_modules/emoji-picker-element-data/en/cldr/data.json'), 'utf8')) as Array<{ emoji: string; skins?: Array<{ emoji: string }> }>
  return data.flatMap(e => [e.emoji, ...(e.skins ?? []).map(s => s.emoji)])
}

/** Forms the picker never sends: typed, from bots, from older clients. */
const CASES = [
  '\u2764', '\u2764\ufe0f', '\u2764\ufe0f\ufe0f', '\u2764\ufe0e', '\u2764\ufe0f\u200d\u{1F525}', '\u2764\u200d\u{1F525}',
  '\u{1F44D}', '\u{1F44D}\ufe0f', '\u{1F44D}\u{1F3FD}', '\u{1F44D}\ufe0f\u{1F3FD}', '\u{1F44D}\u{1F3FD}\ufe0f',
  '\u261d', '\u261d\u{1F3FB}', '\u261d\ufe0f\u{1F3FB}', '1', '1\ufe0f', '1\u20e3', '1\ufe0f\u20e3', '#\u20e3', '*\ufe0f\u20e3', '12',
  '\u00a9', '\u2122\ufe0f', '\u{1F3F3}\u200d\u{1F308}', '\u{1F3F3}\ufe0f\u200d\u{1F308}', '\u{1F441}\u200d\u{1F5E8}',
  '\u{1F1E9}\u{1F1EA}', '\u{1F3F4}\u{E0067}\u{E0062}\u{E0065}\u{E006E}\u{E0067}\u{E007F}',
  '\u{1F468}\u200d\u{1F469}\u200d\u{1F467}', '\u{1F9D1}\u{1F3FE}\u200d\u{1F393}', '+1', 'ok', '\ufe0f',
  '<:party:11111111-2222-3333-4444-555555555555>', '<:x1:11111111-2222-3333-4444-555555555555>'
]

const inputs = (): string[] => [...CASES, ...pickerEmoji()]
const vectors = (): Vector[] => JSON.parse(readFileSync(file, 'utf8')) as Vector[]

describe('reactionForm', () => {
  it.runIf(process.env.REACTION_FORMS_WRITE === '1')('writes the vectors', () => {
    const json = JSON.stringify(inputs().map(emoji => ({ emoji, form: reactionForm(emoji) })), null, 0).replaceAll('},{', '},\n{')
    // ASCII only, so the file reads the same in every editor.
    writeFileSync(file, json.replace(/[^\x20-\x7e\n]/g, c => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0')) + '\n')
  })

  it('matches the shared vectors the server is checked against', () => {
    const v = vectors()
    expect(v.map(x => x.emoji)).toEqual(inputs())
    expect(v.filter(x => reactionForm(x.emoji) !== x.form)).toEqual([])
  })

  it('keeps what the picker sends, except a redundant U+FE0F after a lone emoji', () => {
    const changed = pickerEmoji().filter(e => reactionForm(e) !== e)
    expect(changed.length).toBeGreaterThan(0)
    for (const e of changed) {
      expect([...reactionKey(e)]).toHaveLength(1)
      expect(reactionForm(e)).toBe(reactionKey(e))
    }
  })

  it('makes one reaction of the forms of one emoji and keeps every other emoji apart', () => {
    const picker = pickerEmoji()
    for (const e of picker) expect(reactionForm(reactionKey(e))).toBe(reactionForm(e))
    expect(new Set(picker.map(reactionForm)).size).toBe(new Set(picker).size)
    expect(reactionForm('\u{1F44D}')).toBe(reactionForm('\u{1F44D}\ufe0f'))
    expect(reactionForm('\u2764')).toBe('\u2764\ufe0f')
    expect(reactionForm('\u{1F44D}\u{1F3FD}')).not.toBe(reactionForm('\u{1F44D}'))
    expect(reactionForm('\u2764\ufe0f\u200d\u{1F525}')).not.toBe(reactionForm('\u2764\ufe0f'))
    expect(reactionForm('1')).toBe('1')
  })

  it('holds only characters that are shown as text by default', () => {
    for (let cp = 0; cp < 0x20000; cp++) {
      if (!isTextDefaultEmoji(cp)) continue
      const ch = String.fromCodePoint(cp)
      expect(/\p{Emoji}/u.test(ch) && !/\p{Emoji_Presentation}/u.test(ch), cp.toString(16)).toBe(true)
    }
  })
})
