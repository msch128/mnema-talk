import { afterEach, describe, expect, it, vi } from 'vitest'
import { enhanceEmojiPicker, shortcodeOf, type EmojiPreview } from './emojiPickerExtras'
import { isTypingEvent } from './typingTarget'

class FakePicker extends HTMLElement {
  database?: { getEmojiByUnicodeOrName: (key: string) => Promise<{ shortcodes?: string[] } | null> }
}
customElements.define('extras-fake-picker', FakePicker)

function picker(): FakePicker {
  const el = new FakePicker()
  const root = el.attachShadow({ mode: 'open' })
  root.innerHTML = `
    <input type="search" value="">
    <div class="tabpanel">
      <button class="emoji" role="option" id="tada">🎉</button>
      <button class="emoji" role="option" id="custom" aria-label="party"><img src="http://x/party.png" alt=""></button>
    </div>`
  document.body.appendChild(el)
  return el
}
const root = (el: FakePicker) => el.shadowRoot as ShadowRoot
const over = (el: FakePicker, id: string) => root(el).getElementById(id)?.dispatchEvent(new Event('pointerover', { bubbles: true, composed: true }))

afterEach(() => { document.body.innerHTML = '' })

describe('shortcodeOf', () => {
  it('prefers the picker data and falls back to our table', async () => {
    const el = picker()
    expect(await shortcodeOf(el, '🎉')).toBe(':tada:')
    el.database = { getEmojiByUnicodeOrName: async () => ({ shortcodes: ['partying_face', 'party_face'] }) }
    expect(await shortcodeOf(el, '🥳')).toBe(':partying_face:')
    el.database = { getEmojiByUnicodeOrName: async () => { throw new Error('closed') } }
    expect(await shortcodeOf(el, '🥳')).toBe(':partying_face:')
  })

  it('has no shortcode for a text that is no emoji', async () => {
    expect(await shortcodeOf(picker(), 'zzz')).toBe('')
  })
})

describe('enhanceEmojiPicker', () => {
  it('previews the shortcode of a standard and of a custom emoji, on hover and on focus', async () => {
    const el = picker()
    const seen: (EmojiPreview | null)[] = []
    enhanceEmojiPicker(el, p => seen.push(p))
    over(el, 'tada')
    await vi.waitFor(() => expect(seen.at(-1)).toEqual({ glyph: '🎉', src: '', code: ':tada:' }))
    over(el, 'custom')
    await vi.waitFor(() => expect(seen.at(-1)).toEqual({ glyph: '', src: 'http://x/party.png', code: ':party:' }))
    root(el).getElementById('tada')?.dispatchEvent(new Event('focusin', { bubbles: true, composed: true }))
    await vi.waitFor(() => expect(seen.at(-1)?.code).toBe(':tada:'))
    el.dispatchEvent(new Event('pointerleave'))
    expect(seen.at(-1)).toBeNull()
  })

  it('picks the first result with Enter in the search field only', () => {
    const el = picker()
    enhanceEmojiPicker(el, () => undefined)
    const clicked = vi.fn()
    root(el).getElementById('tada')?.addEventListener('click', clicked)
    const input = root(el).querySelector('input') as HTMLInputElement
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, composed: true, cancelable: true }))
    expect(clicked).toHaveBeenCalledOnce()
    root(el).getElementById('custom')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, composed: true }))
    expect(clicked).toHaveBeenCalledOnce()
  })

  it('clears the search text and reports whether there was any', () => {
    const el = picker()
    const { clearSearch } = enhanceEmojiPicker(el, () => undefined)
    const input = root(el).querySelector('input') as HTMLInputElement
    const onInput = vi.fn()
    input.addEventListener('input', onInput)
    expect(clearSearch()).toBe(false)
    input.value = 'tada'
    expect(clearSearch()).toBe(true)
    expect(input.value).toBe('')
    expect(onInput).toHaveBeenCalledOnce()
  })

  it('stops reacting after detach', () => {
    const el = picker()
    const onPreview = vi.fn()
    enhanceEmojiPicker(el, onPreview).detach()
    over(el, 'tada')
    el.dispatchEvent(new Event('pointerleave'))
    expect(onPreview).not.toHaveBeenCalled()
  })
})

describe('typing in the search', () => {
  it('counts as typing for the shortcut handlers, even retargeted to the host', () => {
    const el = picker()
    const seen: boolean[] = []
    document.addEventListener('keydown', e => seen.push(isTypingEvent(e)), { once: true })
    root(el).querySelector('input')?.dispatchEvent(new KeyboardEvent('keydown', { key: 'm', bubbles: true, composed: true }))
    expect(seen).toEqual([true])
  })
})
