import { describe, expect, it } from 'vitest'
import type { MediaAttachment } from '../types/domain'
import { galleryRows } from './galleryLayout'

const pics = (n: number) => Array.from({ length: n }, (_, i) => ({ id: `p${i}`, original_filename: `${i}.png`, mime_type: 'image/png', size_bytes: 1, url: `/m/${i}`, is_deleted: false }) as MediaAttachment)
const shape = (n: number) => galleryRows(pics(n)).map(r => r.cells.map(c => c.length))

describe('galleryRows', () => {
  it('has no mosaic for fewer than two pictures', () => {
    expect(galleryRows([])).toEqual([])
    expect(galleryRows(pics(1))).toEqual([])
  })

  it('puts two side by side and three as one large and two stacked', () => {
    expect(shape(2)).toEqual([[1, 1]])
    expect(shape(3)).toEqual([[1, 2]])
    expect(galleryRows(pics(3))[0]?.weights).toEqual([2, 1])
  })

  it('lays four to ten out in rows of two or three, keeping every picture in order', () => {
    expect(shape(4)).toEqual([[1, 1], [1, 1]])
    expect(shape(10)).toEqual([[1, 1], [1, 1], [1, 1, 1], [1, 1, 1]])
    const ids = galleryRows(pics(7)).flatMap(r => r.cells.flat().map(a => a.id))
    expect(ids).toEqual(Array.from({ length: 7 }, (_, i) => `p${i}`))
  })

  it('keeps every picture past ten, in rows of three', () => {
    const rows = galleryRows(pics(14))
    expect(rows.flatMap(r => r.cells.flat())).toHaveLength(14)
    expect(rows.at(-1)?.cells).toHaveLength(1)
  })
})
