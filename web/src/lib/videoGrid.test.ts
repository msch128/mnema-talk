import { describe, it, expect } from 'vitest'
import { videoGridLayout, videoGridSize, TILE_ASPECT } from './videoGrid'

const layout = (count: number, width: number | undefined, height: number, opts: Partial<NonNullable<Parameters<typeof videoGridLayout>[0]>> = {}) => videoGridLayout({ count, ...(width === undefined ? {} : { width }), height, ...opts })

describe('videoGridLayout basics', () => {
  it('lays out nothing for no tiles', () => {
    expect(layout(0, 1000, 600)).toEqual({ cols: 0, rows: 0, tileWidth: 0, tileHeight: 0, overflow: false })
    expect(layout(-3, 1000, 600).cols).toBe(0)
    expect(layout(NaN, 1000, 600).cols).toBe(0)
    expect(videoGridLayout().cols).toBe(0)
  })

  it('one tile fills the area at 16:9, limited by the height in a wide area', () => {
    expect(layout(1, 1600, 900)).toMatchObject({ cols: 1, rows: 1, tileWidth: 1600, tileHeight: 900 })
    expect(layout(1, 2000, 450)).toMatchObject({ cols: 1, rows: 1, tileWidth: 800, tileHeight: 450 })
    expect(layout(1, 800, 2000)).toMatchObject({ cols: 1, rows: 1, tileWidth: 800, tileHeight: 450 })
  })

  it('two tiles sit side by side in a wide area and stack in a tall one', () => {
    expect(layout(2, 1600, 450)).toMatchObject({ cols: 2, rows: 1, tileWidth: 800 })
    expect(layout(2, 800, 1000)).toMatchObject({ cols: 1, rows: 2, tileWidth: 800 })
  })

  it('picks the column count with the widest tile', () => {
    // 4 tiles in 1600x900: 2x2 gives 800 wide tiles, 4x1 only 400.
    expect(layout(4, 1600, 900)).toMatchObject({ cols: 2, rows: 2, tileWidth: 800, tileHeight: 450 })
    // 3 tiles in a very wide strip: one row.
    expect(layout(3, 3000, 400)).toMatchObject({ cols: 3, rows: 1 })
    // 6 tiles in 1600x900: 3x2 (533) beats 2x3 (533 limited by 300 height) and 6x1.
    expect(layout(6, 1600, 900)).toMatchObject({ cols: 3, rows: 2, tileWidth: 533 })
    // 9 in a 16:9 area: 3x3.
    expect(layout(9, 1600, 900)).toMatchObject({ cols: 3, rows: 3, tileWidth: 533 })
    // In a square area two wide columns beat 3x3 (426 vs 400).
    expect(layout(9, 1200, 1200)).toMatchObject({ cols: 2, rows: 5, tileWidth: 426 })
  })

  it('accounts for the gaps between tiles', () => {
    const r = layout(2, 1012, 300, { gap: 12 })
    expect(r).toMatchObject({ cols: 2, rows: 1, tileWidth: 500 })
    const size = videoGridSize(r, 12)
    expect(size.width).toBe(1012)
    expect(size.width).toBeLessThanOrEqual(1012)
  })

  it('caps the tile width so one or two people do not get huge', () => {
    expect(layout(1, 1600, 900, { maxTileWidth: 640 })).toMatchObject({ cols: 1, tileWidth: 640, tileHeight: 360 })
    const two = layout(2, 1600, 900, { maxTileWidth: 640 })
    expect(two.tileWidth).toBe(640)
  })

  it('breaks ties between capped layouts by the shape of the area', () => {
    // Every layout of 4 tiles reaches the cap; a square area gets 2x2,
    // a wide one a single row.
    expect(layout(4, 2000, 2000, { maxTileWidth: 400 })).toMatchObject({ cols: 2, rows: 2, tileWidth: 400 })
    expect(layout(4, 4000, 500, { maxTileWidth: 400 })).toMatchObject({ cols: 4, rows: 1, tileWidth: 400 })
    expect(layout(4, 500, 4000, { maxTileWidth: 400 })).toMatchObject({ cols: 1, rows: 4, tileWidth: 400 })
  })

  it('uses another aspect ratio when asked and ignores a broken one', () => {
    expect(layout(1, 1000, 1000, { aspect: 1 })).toMatchObject({ tileWidth: 1000, tileHeight: 1000 })
    expect(layout(1, 1600, 900, { aspect: NaN })).toMatchObject({ tileWidth: 1600, tileHeight: 900 })
    expect(layout(1, 1600, 900, { aspect: -2 })).toMatchObject({ tileWidth: 1600, tileHeight: 900 })
  })

  it('ignores a broken maximum', () => {
    expect(layout(1, 1600, 900, { maxTileWidth: 0 }).tileWidth).toBe(1600)
    // Exercise the defensive boundary against a malformed configuration value.
    const malformed: unknown = { count: 1, width: 1600, height: 900, maxTileWidth: null }
    const result: unknown = Reflect.apply(videoGridLayout, undefined, [malformed])
    expect(result).toMatchObject({ tileWidth: 1600 })
  })

  it('returns whole pixels', () => {
    const r = layout(3, 1001, 701, { gap: 7 })
    expect(Number.isInteger(r.tileWidth)).toBe(true)
    expect(Number.isInteger(r.tileHeight)).toBe(true)
  })

  it('without a size (not laid out yet) gives a near-square column count and no tile size', () => {
    expect(layout(5, 0, 0)).toEqual({ cols: 3, rows: 2, tileWidth: 0, tileHeight: 0, overflow: false })
    expect(layout(1, 1000, 0)).toMatchObject({ cols: 1, rows: 1, tileWidth: 0 })
    expect(layout(4, undefined, 500)).toMatchObject({ cols: 2, rows: 2, tileWidth: 0 })
  })
})

describe('videoGridLayout minimum tile width', () => {
  it('keeps the best layout when it is wide enough', () => {
    expect(layout(4, 1600, 900, { minTileWidth: 200 })).toMatchObject({ cols: 2, tileWidth: 800, overflow: false })
  })

  it('falls back to as many minimum-width columns as fit and scrolls', () => {
    const r = layout(30, 800, 300, { gap: 10, minTileWidth: 180 })
    expect(r.tileWidth).toBe(180)
    // 4 x 180 + 3 x 10 = 750 <= 800 < 5 x 180 + 4 x 10
    expect(r.cols).toBe(4)
    expect(r.rows).toBe(8)
    expect(r.overflow).toBe(true)
  })

  it('never makes a tile wider than the area, also with a large minimum', () => {
    const r = layout(3, 150, 100, { minTileWidth: 400 })
    expect(r).toMatchObject({ cols: 1, rows: 3, tileWidth: 150 })
    expect(r.overflow).toBe(true)
  })

  it('a minimum above the maximum is clamped to it', () => {
    const r = layout(2, 1600, 900, { minTileWidth: 900, maxTileWidth: 500 })
    expect(r.tileWidth).toBe(500)
    expect(r.overflow).toBe(false)
  })

  it('handles gaps larger than the area', () => {
    const r = layout(3, 100, 100, { gap: 500, minTileWidth: 50 })
    expect(r.cols).toBe(1)
    expect(r.tileWidth).toBe(50)
  })
})

describe('videoGridLayout invariants over many sizes', () => {
  // Brute force reference: the widest tile any column count reaches.
  function widestTile(n: number, W: number, H: number, gap: number, maxW: number) {
    let best = 0
    for (let cols = 1; cols <= n; cols++) {
      const rows = Math.ceil(n / cols)
      const w = Math.floor(Math.min((W - gap * (cols - 1)) / cols, ((H - gap * (rows - 1)) / rows) * TILE_ASPECT, maxW))
      best = Math.max(best, w)
    }
    return best
  }

  const sizes: Array<[number, number]> = [[320, 180], [640, 480], [1000, 600], [1280, 720], [1920, 1080], [600, 1200], [2560, 600], [777, 333]]
  const gaps = [0, 8, 12]
  const caps = [Infinity, 640, 960]

  it('fits every tile into the area with the widest possible tile', () => {
    let checked = 0
    for (let n = 1; n <= 30; n++) {
      for (const [W, H] of sizes) {
        for (const gap of gaps) {
          for (const maxTileWidth of caps) {
            const r = layout(n, W, H, { gap, maxTileWidth })
            const where = `${n} tiles in ${W}x${H}, gap ${gap}, cap ${maxTileWidth}`
            expect(r.cols * r.rows, where).toBeGreaterThanOrEqual(n)
            // No empty row.
            expect(r.cols * (r.rows - 1), where).toBeLessThan(n)
            expect(r.rows, where).toBe(Math.ceil(n / r.cols))
            expect(r.tileWidth, where).toBeLessThanOrEqual(maxTileWidth)
            expect(r.tileHeight, where).toBe(Math.floor(r.tileWidth / TILE_ASPECT))
            const size = videoGridSize(r, gap)
            expect(size.width, where).toBeLessThanOrEqual(W)
            expect(size.height, where).toBeLessThanOrEqual(H)
            expect(r.tileWidth, where).toBe(widestTile(n, W, H, gap, maxTileWidth))
            expect(r.overflow, where).toBe(false)
            checked++
          }
        }
      }
    }
    expect(checked).toBe(30 * sizes.length * gaps.length * caps.length)
  })

  it('never shrinks a tile when the area grows', () => {
    for (let n = 1; n <= 16; n++) {
      let prev = 0
      for (let W = 200; W <= 2400; W += 100) {
        const r = layout(n, W, Math.round(W * 0.6), { gap: 12 })
        expect(r.tileWidth, `${n} tiles at ${W}`).toBeGreaterThanOrEqual(prev)
        prev = r.tileWidth
      }
    }
  })
})

describe('videoGridSize', () => {
  it('adds up tiles and gaps', () => {
    expect(videoGridSize({ cols: 3, rows: 2, tileWidth: 100, tileHeight: 56 }, 10)).toEqual({ width: 320, height: 122 })
    expect(videoGridSize({ cols: 1, rows: 1, tileWidth: 100, tileHeight: 56 })).toEqual({ width: 100, height: 56 })
  })

  it('is empty without a layout', () => {
    expect(videoGridSize({ cols: 0, rows: 0, tileWidth: 0, tileHeight: 0 }, 10)).toEqual({ width: 0, height: 0 })
    expect(videoGridSize({ cols: 2, rows: 1, tileWidth: 0, tileHeight: 0 }, 10)).toEqual({ width: 0, height: 0 })
  })
})

it('returns a bounded fallback for an area too small to produce one whole video pixel', () => {
  expect(videoGridLayout({ count: 2, width: 0.1, height: 0.1 })).toEqual({ cols: 1, rows: 2, tileWidth: 1, tileHeight: 0, overflow: false })
})
