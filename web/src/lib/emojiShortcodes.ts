// The standard emoji behind ":smile:" suggestions. The table is a generated
// JSON file (scripts/genEmojiShortcodes.mjs), loaded on first use so it stays
// out of the main bundle.
import { shallowRef } from 'vue'

let table: Promise<Readonly<Record<string, string>>> | null = null

export function loadEmojiShortcodes(): Promise<Readonly<Record<string, string>>> {
  table ??= import('./emojiShortcodes.json').then(m => m.default as Record<string, string>)
  return table
}

/** The table as a ref (empty until ensure() has loaded it). */
export function useEmojiShortcodes() {
  const table = shallowRef<Readonly<Record<string, string>>>({})
  async function ensure() {
    if (Object.keys(table.value).length) return
    try {
      table.value = await loadEmojiShortcodes()
    } catch {
      // Without the table only the custom emoji are suggested.
    }
  }
  return { table, ensure }
}
