import { describe, expect, it } from 'vitest'
import { uploadPatch } from './roleSave'

describe('role editor upload limit', () => {
  it('sends the upload limit only when the Owner changed it', () => {
    expect(uploadPatch(false, 20, null)).toEqual({})
    expect(uploadPatch(true, undefined, 5)).toEqual({})
    expect(uploadPatch(true, 5, 5)).toEqual({})
    expect(uploadPatch(true, 20, 5)).toEqual({ max_upload_mb: 20 })
    expect(uploadPatch(true, null, 5)).toEqual({ max_upload_mb: null })
  })
})
