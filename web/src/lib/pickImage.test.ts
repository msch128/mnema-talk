import { describe, it, expect } from 'vitest'
import { IMAGE_UPLOAD_MAX_MB, pickImage } from './pickImage'

function inputWith(file?: File): HTMLInputElement {
  const input = document.createElement('input')
  input.type = 'file'
  if (file) {
    const dt = new DataTransfer()
    dt.items.add(file)
    input.files = dt.files
  }
  return input
}

describe('pickImage', () => {
  it('returns the chosen file and clears the input so it can be picked again', () => {
    const file = new File(['x'], 'a.png', { type: 'image/png' })
    const input = inputWith(file)
    expect(pickImage(input)).toEqual({ file, tooBig: false })
    expect(input.files).toHaveLength(0)
  })

  it('refuses a file over the limit and still clears the input', () => {
    const big = new File([new Uint8Array(IMAGE_UPLOAD_MAX_MB * 1024 * 1024 + 1)], 'big.png', { type: 'image/png' })
    const input = inputWith(big)
    expect(pickImage(input)).toEqual({ file: null, tooBig: true })
    expect(input.files).toHaveLength(0)
  })

  it('returns nothing when nothing was chosen', () => {
    expect(pickImage(inputWith())).toEqual({ file: null, tooBig: false })
  })
})
