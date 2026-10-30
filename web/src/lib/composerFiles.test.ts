import { describe, expect, it } from 'vitest'
import { dragHasFiles, growHeight, pastedFiles } from './composerFiles'

const png = new File(['x'], 'a.png', { type: 'image/png' })

function transfer(files: File[], data: Record<string, string> = {}, types: string[] = []) {
  return { files, types, getData: (k: string) => data[k] ?? '' } as unknown as DataTransfer
}

describe('growHeight', () => {
  it('follows the content up to the share of the window', () => {
    expect(growHeight(24, 1000)).toEqual({ height: 24, scrolls: false })
    expect(growHeight(300, 1000)).toEqual({ height: 300, scrolls: false })
  })
  it('caps at 45 percent of the window and then scrolls', () => {
    expect(growHeight(900, 1000)).toEqual({ height: 450, scrolls: true })
    expect(growHeight(900, 600, 0.5)).toEqual({ height: 300, scrolls: true })
  })
})

describe('pastedFiles', () => {
  it('returns the files of a copied image', () => {
    expect(pastedFiles(transfer([png]))).toEqual([png])
  })
  it('leaves plain text alone', () => {
    expect(pastedFiles(transfer([], { 'text/plain': 'hello' }))).toEqual([])
    expect(pastedFiles(null)).toEqual([])
  })
  it('keeps a rendered picture next to text as text', () => {
    expect(pastedFiles(transfer([png], { 'text/html': '<table></table>', 'text/plain': 'a\tb' }))).toEqual([])
  })
  it('attaches a copied web image whose html has no text', () => {
    expect(pastedFiles(transfer([png], { 'text/html': '<img src="x">' }))).toEqual([png])
  })
})

describe('dragHasFiles', () => {
  it('is true only for a file drag', () => {
    expect(dragHasFiles(transfer([], {}, ['Files']))).toBe(true)
    expect(dragHasFiles(transfer([], {}, ['text/plain']))).toBe(false)
    expect(dragHasFiles(null)).toBe(false)
  })
})
