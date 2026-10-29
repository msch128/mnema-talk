// The mosaic shape of a message's pictures (two or more): which pictures share
// a row and how tall the row is. Rows have a fixed aspect ratio, so nothing
// moves while the pictures load. Tailwind needs the class names written out.
import type { MediaAttachment } from '../types/domain'

export interface GalleryRow {
  rowClass: string
  /** Each cell holds one picture, or two stacked on top of each other. */
  cells: MediaAttachment[][]
  /** Relative width of each cell (flex-grow). */
  weights: number[]
}

// Pictures per row for 4 and up; two and three have their own shape below.
const ROWS: Record<number, number[]> = {
  4: [2, 2], 5: [2, 3], 6: [3, 3], 7: [2, 2, 3], 8: [2, 3, 3], 9: [3, 3, 3], 10: [2, 2, 3, 3]
}

const TWO_UP = 'aspect-[5/2]'
const THREE_UP = 'aspect-[4/1]'

export function galleryRows(items: MediaAttachment[]): GalleryRow[] {
  const n = items.length
  if (n < 2) return []
  const one = (a: MediaAttachment) => [a]
  if (n === 2) return [{ rowClass: 'aspect-[2/1]', cells: items.map(one), weights: [1, 1] }]
  if (n === 3) {
    // One large picture on the left, two stacked on the right.
    return [{ rowClass: 'aspect-[3/2]', cells: [[items[0]!], [items[1]!, items[2]!]], weights: [2, 1] }]
  }
  const rows: GalleryRow[] = []
  let at = 0
  for (const size of ROWS[Math.min(n, 10)] ?? [3]) {
    const chunk = items.slice(at, at + size)
    at += size
    rows.push({ rowClass: size === 2 ? TWO_UP : THREE_UP, cells: chunk.map(one), weights: chunk.map(() => 1) })
  }
  // More than the server allows: keep every picture, in rows of three.
  while (at < n) {
    const chunk = items.slice(at, at + 3)
    at += 3
    rows.push({ rowClass: THREE_UP, cells: chunk.map(one), weights: chunk.map(() => 1) })
  }
  return rows
}

export const GALLERY_CELL = {
  button: 'group relative block overflow-hidden bg-mnema-raised focus:outline-hidden focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent',
  // Absolutely placed, so the picture's own size never stretches the row.
  image: 'absolute inset-0 h-full w-full cursor-pointer object-cover transition group-hover:opacity-95',
  // A small chip in the corner; hidden on touch, where there is no hover.
  chip: 'pointer-events-none absolute right-1.5 top-1.5 flex h-6 w-6 items-center justify-center rounded-md bg-black/50 text-white opacity-0 transition-opacity motion-reduce:transition-none group-hover:opacity-100 group-focus-visible:opacity-100 [@media(pointer:coarse)]:hidden'
} as const
