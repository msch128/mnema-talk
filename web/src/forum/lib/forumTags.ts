// The colours a forum tag can have. The server stores a palette name, never CSS;
// the classes are written out in full so the stylesheet generator finds them.
export const TAG_COLORS = ['gray', 'red', 'orange', 'yellow', 'green', 'teal', 'blue', 'purple', 'pink'] as const
export type TagColor = typeof TAG_COLORS[number]

const PLAIN = 'bg-mnema-raised text-mnema-secondary border-mnema-hairline'

const CHIP: Record<TagColor, string> = {
  gray: 'bg-zinc-400/10 text-zinc-300 border-zinc-400/25',
  red: 'bg-red-400/10 text-red-300 border-red-400/25',
  orange: 'bg-orange-400/10 text-orange-300 border-orange-400/25',
  yellow: 'bg-amber-400/10 text-amber-300 border-amber-400/25',
  green: 'bg-emerald-400/10 text-emerald-300 border-emerald-400/25',
  teal: 'bg-teal-400/10 text-teal-300 border-teal-400/25',
  blue: 'bg-sky-400/10 text-sky-300 border-sky-400/25',
  purple: 'bg-violet-400/10 text-violet-300 border-violet-400/25',
  pink: 'bg-pink-400/10 text-pink-300 border-pink-400/25'
}

const SWATCH: Record<TagColor, string> = {
  gray: 'bg-zinc-400',
  red: 'bg-red-400',
  orange: 'bg-orange-400',
  yellow: 'bg-amber-400',
  green: 'bg-emerald-400',
  teal: 'bg-teal-400',
  blue: 'bg-sky-400',
  purple: 'bg-violet-400',
  pink: 'bg-pink-400'
}

function known(color: string | undefined): color is TagColor {
  return !!color && Object.hasOwn(CHIP, color)
}

/** Classes of a tag chip; an unknown or missing colour is the plain look. */
export function tagChipClass(color?: string): string {
  return known(color) ? CHIP[color] : PLAIN
}

/** Class of the small round swatch that shows a colour (the plain look has none). */
export function tagSwatchClass(color?: string): string {
  return known(color) ? SWATCH[color] : 'bg-mnema-border-strong'
}

/** The most tags one post can carry (the server's MaxPostTags). */
export const MAX_POST_TAGS = 5
/** The most tags one forum can define. */
export const MAX_FORUM_TAGS = 20
