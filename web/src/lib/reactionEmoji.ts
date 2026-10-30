// What a reaction is sent as. The pickers hand over a Unicode emoji or the
// token of a custom emoji; a typed or scripted :name: (":party:", ":tada:") is
// resolved here, so a reaction never reaches the server as a bare shortcode.
import { loadEmojiShortcodes } from './emojiShortcodes'
import { emojiToken, type CustomEmoji } from './customEmoji'

const SHORTCODE = /^:([a-z0-9_+-]{1,64}):$/

/** A :name: of a custom emoji as its token; anything else as given. */
export function customReaction(raw: string, byName: ReadonlyMap<string, CustomEmoji>): string {
  const emoji = raw.trim()
  const name = SHORTCODE.exec(emoji)?.[1]
  const custom = name ? byName.get(name) : undefined
  return custom ? emojiToken(custom) : emoji
}

/** Like customReaction, and a :name: of a standard emoji as that emoji (a
 * custom emoji of the community wins over a standard one of the same name). */
export function resolveReaction(raw: string, byName: ReadonlyMap<string, CustomEmoji>): string | Promise<string> {
  const emoji = customReaction(raw, byName)
  const name = SHORTCODE.exec(emoji)?.[1]
  // Only a shortcode waits for the table; every other reaction goes out at once.
  return name ? standardReaction(name, emoji) : emoji
}

async function standardReaction(name: string, fallback: string): Promise<string> {
  try {
    return (await loadEmojiShortcodes())[name] ?? fallback
  } catch {
    return fallback
  }
}
