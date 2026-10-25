// Regenerates src/lib/emojiShortcodes.json (":smile:" -> glyph) from the
// emoji-picker-element-data GitHub shortcode set. Run after bumping that package:
//   node scripts/genEmojiShortcodes.ts
import { readFileSync, writeFileSync } from 'node:fs'

const source = new URL('../node_modules/emoji-picker-element-data/en/github/data.json', import.meta.url)
const target = new URL('../src/lib/emojiShortcodes.json', import.meta.url)

const table: Record<string, string> = {}
const items = JSON.parse(readFileSync(source, 'utf8')) as Array<{ emoji: string; shortcodes?: string[] }>
for (const item of items) {
  for (const code of item.shortcodes ?? []) table[code] = item.emoji
}
const sorted = Object.fromEntries(Object.entries(table).sort(([a], [b]) => (a < b ? -1 : 1)))
writeFileSync(target, JSON.stringify(sorted) + '\n')
console.log(`${Object.keys(sorted).length} shortcodes`)
