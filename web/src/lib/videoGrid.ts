// Layout of the Talk's participant grid, like Discord's: every tile is 16:9
// and as large as possible while all of them fit into the area.

export const TILE_ASPECT = 16 / 9

function finite(n: number | undefined, fallback: number): number {
  return typeof n === 'number' && Number.isFinite(n) ? n : fallback
}

/**
 * Columns, rows and tile size for `count` tiles in a `width` x `height` area.
 *
 * Tries every column count: rows = ceil(count / cols), and a tile is as wide
 * as both the column width and the row height (at the aspect ratio) allow.
 * The widest tile wins. Tiles capped at maxTileWidth tie often; then the
 * layout whose shape is closest to the area's wins (four capped tiles are
 * 2x2 in a square area and one row in a wide one).
 *
 * Options:
 *   gap           space between tiles (px)
 *   aspect        tile width / height
 *   maxTileWidth  tiles never get wider (one or two people are not huge)
 *   minTileWidth  tiles never get narrower; when even that does not fit, as
 *                 many columns as fit are used and the grid scrolls
 *                 (`overflow: true`)
 *
 * Sizes are whole pixels (floored), so the tiles plus gaps never exceed the
 * area through rounding. An area without a size (not laid out yet) gives
 * `tileWidth: 0` and a near-square column count.
 */
export interface GridOptions { count?: number; width?: number; height?: number; gap?: number; aspect?: number; maxTileWidth?: number; minTileWidth?: number }
export interface GridLayout { cols: number; rows: number; tileWidth: number; tileHeight: number; overflow: boolean }
export function videoGridLayout({
  count,
  width,
  height,
  gap = 0,
  aspect = TILE_ASPECT,
  maxTileWidth = Infinity,
  minTileWidth = 0
}: GridOptions = {}): GridLayout {
  const n = Math.max(0, Math.floor(finite(count, 0)))
  const W = Math.max(0, finite(width, 0))
  const H = Math.max(0, finite(height, 0))
  const g = Math.max(0, finite(gap, 0))
  const a = finite(aspect, TILE_ASPECT)
  const ratio = a > 0 ? a : TILE_ASPECT
  const maxW = finite(maxTileWidth, Infinity) > 0 ? finite(maxTileWidth, Infinity) : Infinity
  const minW = Math.min(Math.max(0, finite(minTileWidth, 0)), maxW)

  if (!n) return { cols: 0, rows: 0, tileWidth: 0, tileHeight: 0, overflow: false }
  if (!W || !H) {
    const cols = Math.ceil(Math.sqrt(n))
    return { cols, rows: Math.ceil(n / cols), tileWidth: 0, tileHeight: 0, overflow: false }
  }

  let best: { cols: number; rows: number; tileWidth: number; shape: number } | null = null
  for (let cols = 1; cols <= n; cols++) {
    const rows = Math.ceil(n / cols)
    const colWidth = (W - g * (cols - 1)) / cols
    const rowHeight = (H - g * (rows - 1)) / rows
    if (colWidth <= 0 || rowHeight <= 0) continue
    const tileWidth = Math.floor(Math.min(colWidth, rowHeight * ratio, maxW))
    // How far the grid's shape (cols x rows tiles) is from the area's.
    const shape = Math.abs(Math.log((cols * ratio) / rows / (W / H)))
    if (!best || tileWidth > best.tileWidth || (tileWidth === best.tileWidth && shape < best.shape)) {
      best = { cols, rows, tileWidth, shape }
    }
  }

  if (best && best.tileWidth >= minW && best.tileWidth > 0) {
    const { cols, rows, tileWidth } = best
    return { cols, rows, tileWidth, tileHeight: Math.floor(tileWidth / ratio), overflow: false }
  }

  // Too small for the minimum: as many minimum-width columns as fit, the
  // rest scrolls.
  const tileWidth = Math.max(1, Math.floor(Math.min(minW || W, W)))
  const cols = Math.max(1, Math.min(n, Math.floor((W + g) / (tileWidth + g))))
  const rows = Math.ceil(n / cols)
  const tileHeight = Math.floor(tileWidth / ratio)
  return { cols, rows, tileWidth, tileHeight, overflow: rows * tileHeight + (rows - 1) * g > H }
}

/** Outer size of the laid-out grid (tiles plus gaps), for centering. */
export function videoGridSize({ cols, rows, tileWidth, tileHeight }: Pick<GridLayout, 'cols' | 'rows' | 'tileWidth' | 'tileHeight'>, gap = 0): { width: number; height: number } {
  if (!cols || !rows || !tileWidth) return { width: 0, height: 0 }
  return {
    width: cols * tileWidth + (cols - 1) * gap,
    height: rows * tileHeight + (rows - 1) * gap
  }
}
