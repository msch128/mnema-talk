// Custom emoji in text. A message stores a stable token, <:name:id>: renaming
// or deleting an emoji never breaks old text, it only changes what is drawn.
// Members type :name:, which is turned into the token when the message is sent.

import { registerTextRule } from './markdown/rules'

export interface CustomEmoji {
  id: string
  name: string
  animated: boolean
  url: string
  still_url?: string
}

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'
const NAME = '[a-z0-9_]{2,32}'
// The token as written in the message.
const TOKEN = new RegExp(`<:(${NAME}):(${UUID})>`, 'g')
const WHOLE_TOKEN = new RegExp(`^<:(${NAME}):(${UUID})>$`)
const SHORTCODE = new RegExp(`(?<![A-Za-z0-9_:<]):(${NAME}):(?![A-Za-z0-9_])`, 'g')
// Code is shown as typed, so nothing inside it is converted.
const CODE_SPANS = /(```[\s\S]*?```|`[^`\n]+`)/

export function emojiToken(emoji: Pick<CustomEmoji, 'id' | 'name'>): string {
  return `<:${emoji.name}:${emoji.id}>`
}

/** The name and id of a token that is exactly one custom emoji, or null. */
export function parseEmojiToken(text: string): { name: string; id: string } | null {
  const m = WHOLE_TOKEN.exec(text)
  return m ? { name: m[1] as string, id: m[2] as string } : null
}

/** Tokens as :name:, for places that show text without images. */
export function plainEmojiText(text: string): string {
  return text.replace(TOKEN, ':$1:')
}

/** Tokens as :name: while editing, so the textarea stays readable. */
export const shortcodesForEditing = plainEmojiText

/** Replaces :name: of known emoji by their token, outside of code. */
export function encodeShortcodes(text: string, byName: ReadonlyMap<string, CustomEmoji>): string {
  if (!byName.size || !text.includes(':')) return text
  return text.split(CODE_SPANS).map((part, i) => i % 2 === 1
    ? part
    : part.replace(SHORTCODE, (all: string, name: string) => {
      const emoji = byName.get(name)
      return emoji ? emojiToken(emoji) : all
    })).join('')
}

export interface EmojiImage {
  src: string
  alt: string
}

/** The picture to show for an emoji: the still frame when motion is unwelcome. */
export function emojiImage(emoji: CustomEmoji, reducedMotion: boolean): EmojiImage {
  return { src: reducedMotion && emoji.still_url ? emoji.still_url : emoji.url, alt: `:${emoji.name}:` }
}

export type EmojiLookup = (id: string) => CustomEmoji | undefined

let lookup: EmojiLookup = () => undefined
let reducedMotion = (): boolean => false

/** Where the markdown rule finds emoji by id, and whether motion is unwelcome (set by the emoji store). */
export function setEmojiSource(find: EmojiLookup, reduced: () => boolean) {
  lookup = find
  reducedMotion = reduced
}

// Custom emoji tokens in message text: an image, or :name: when the emoji is
// gone or not loaded yet. Previews (replies, notifications) show :name:.
registerTextRule({
  re: TOKEN,
  build(m) {
    const [, name = '', id = ''] = m
    const emoji = lookup(id)
    if (!emoji) return [{ t: 'text', v: `:${name}:` }]
    const img = emojiImage(emoji, reducedMotion())
    return [{ t: 'el', tag: 'img', attrs: { class: 'md-emoji', src: img.src, alt: img.alt, title: img.alt, draggable: 'false', loading: 'lazy' }, c: [] }]
  },
  plain: m => `:${m[1] ?? ''}:`,
  inLinks: true
})
