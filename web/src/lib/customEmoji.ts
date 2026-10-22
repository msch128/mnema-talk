// Custom emoji in text. A message stores a stable token, <:name:id>: renaming
// or deleting an emoji never breaks old text, it only changes what is drawn.
// Members type :name:, which is turned into the token when the message is sent.

export interface CustomEmoji {
  id: string
  name: string
  animated: boolean
  url: string
  still_url?: string
}

const UUID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'
const NAME = '[a-z0-9_]{2,32}'
// The token as written in the message, and as it looks after HTML escaping.
const TOKEN = new RegExp(`<:(${NAME}):(${UUID})>`, 'g')
const ESCAPED_TOKEN = new RegExp(`&lt;:(${NAME}):(${UUID})&gt;`, 'g')
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

function attr(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;')
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

/**
 * Draws the tokens of already HTML-escaped text as small images; a token whose
 * emoji is gone (or not loaded yet) shows as :name:. Only fixed markup with the
 * server's own image address is produced.
 */
export function renderEmojiTokens(escapedHtml: string, lookup: EmojiLookup, reducedMotion: boolean): string {
  if (!escapedHtml.includes('&lt;:')) return escapedHtml
  return escapedHtml.replace(ESCAPED_TOKEN, (_all: string, name: string, id: string) => {
    const emoji = lookup(id)
    if (!emoji) return `:${name}:`
    const img = emojiImage(emoji, reducedMotion)
    return `<img class="md-emoji" src="${attr(img.src)}" alt="${img.alt}" title="${img.alt}" draggable="false" loading="lazy" decoding="async">`
  })
}
