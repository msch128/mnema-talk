// What the emoji picker (emoji-picker-element, in a shadow root) lacks and a
// chat expects: a preview of the emoji under the pointer or the keyboard focus
// with its :shortcode:, Enter on the search field choosing the first result,
// and Escape that clears the search before it closes the picker.
import { loadEmojiShortcodes } from './emojiShortcodes'

export interface EmojiPreview {
  /** The emoji as text; empty for a custom emoji, which has an image. */
  glyph: string
  /** The image of a custom emoji. */
  src: string
  /** The shortcode with its colons, e.g. ":tada:". */
  code: string
}

interface PickerDatabase {
  getEmojiByUnicodeOrName(key: string): Promise<{ shortcodes?: string[] } | null>
}
type PickerLike = HTMLElement & { database?: PickerDatabase }

const VARIATION_SELECTOR = /️/g
const OPTION = '[role="option"]'

function searchField(picker: HTMLElement): HTMLInputElement | null {
  return picker.shadowRoot?.querySelector<HTMLInputElement>('input[type="search"], input') ?? null
}

/** The shortcode of a standard emoji: our (English) table first, as it is what is typed in a message; the picker's own data, which follows the display language, for what the table lacks. */
export async function shortcodeOf(picker: PickerLike, glyph: string): Promise<string> {
  try {
    const bare = glyph.replace(VARIATION_SELECTOR, '')
    const names = Object.entries(await loadEmojiShortcodes()).filter(([, value]) => value.replace(VARIATION_SELECTOR, '') === bare)
    // Aliases such as "+1" come after the plain words; the first word is the usual name.
    const best = names.find(([name]) => /^[a-z]/.test(name)) ?? names[0]
    if (best) return `:${best[0]}:`
  } catch {
    // The picker's data answers.
  }
  try {
    const code = (await picker.database?.getEmojiByUnicodeOrName(glyph))?.shortcodes?.[0]
    if (code) return `:${code}:`
  } catch {
    // No shortcode.
  }
  return ''
}

/** The emoji a pointer or focus event is on, as the preview shows it. */
async function previewOf(picker: PickerLike, event: Event): Promise<EmojiPreview | null> {
  const origin = event.composedPath()[0]
  const button = origin instanceof Element ? origin.closest('button.emoji') : null
  if (!button) return null
  const image = button.querySelector('img')
  if (image) {
    const name = button.getAttribute('aria-label') ?? image.alt
    return name ? { glyph: '', src: image.src, code: `:${name}:` } : null
  }
  const glyph = button.textContent?.trim() ?? ''
  return glyph ? { glyph, src: '', code: await shortcodeOf(picker, glyph) } : null
}

/**
 * Wires the extras to a picker. `onPreview` gets the emoji under the pointer or
 * focus (null when there is none). Returns `clearSearch` (true when there was
 * text to clear) and a function that removes everything again.
 */
export function enhanceEmojiPicker(picker: PickerLike, onPreview: (preview: EmojiPreview | null) => void) {
  const root = picker.shadowRoot
  let latest = 0
  const show = (event: Event) => {
    const mine = ++latest
    void previewOf(picker, event).then(preview => { if (mine === latest && preview) onPreview(preview) })
  }
  const hide = () => { latest++; onPreview(null) }

  function clearSearch(): boolean {
    const input = searchField(picker)
    if (!input?.value) return false
    input.value = ''
    input.dispatchEvent(new Event('input', { bubbles: true }))
    return true
  }

  function onKeydown(event: Event) {
    if (!(event instanceof KeyboardEvent) || event.key !== 'Enter' || event.isComposing) return
    const origin = event.composedPath()[0]
    if (!(origin instanceof HTMLInputElement)) return
    const first = root?.querySelector<HTMLElement>(`.tabpanel ${OPTION}`) ?? root?.querySelector<HTMLElement>(OPTION)
    if (!first) return
    event.preventDefault()
    first.click()
  }

  root?.addEventListener('pointerover', show)
  root?.addEventListener('focusin', show)
  root?.addEventListener('keydown', onKeydown)
  picker.addEventListener('pointerleave', hide)
  root?.addEventListener('focusout', hide)
  return {
    clearSearch,
    detach() {
      root?.removeEventListener('pointerover', show)
      root?.removeEventListener('focusin', show)
      root?.removeEventListener('keydown', onKeydown)
      picker.removeEventListener('pointerleave', hide)
      root?.removeEventListener('focusout', hide)
    }
  }
}
