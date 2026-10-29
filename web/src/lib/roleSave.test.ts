import { describe, expect, it } from 'vitest'
import { customNameMissing, ownerUploadRejected, saveClosed, uploadPatch } from './roleSave'
import type { Role } from '../types/domain'

const role = (is_everyone: boolean) => ({ is_everyone }) as Role

describe('role editor save decisions', () => {
  it('stays closed without a role or while a save runs', () => {
    expect(saveClosed(null, false)).toBe(true)
    expect(saveClosed(role(false), true)).toBe(true)
    expect(saveClosed(role(false), false)).toBe(false)
  })

  it('asks for a name from custom roles only', () => {
    expect(customNameMissing(role(false), '')).toBe(true)
    expect(customNameMissing(role(false), 'Mods')).toBe(false)
    expect(customNameMissing(role(true), '')).toBe(false)
  })

  it('rejects an unreadable upload limit from the Owner only', () => {
    expect(ownerUploadRejected(true, undefined)).toBe(true)
    expect(ownerUploadRejected(true, null)).toBe(false)
    expect(ownerUploadRejected(true, 20)).toBe(false)
    expect(ownerUploadRejected(false, undefined)).toBe(false)
  })

  it('sends the upload limit only when the Owner changed it', () => {
    expect(uploadPatch(false, 20, null)).toEqual({})
    expect(uploadPatch(true, undefined, 5)).toEqual({})
    expect(uploadPatch(true, 5, 5)).toEqual({})
    expect(uploadPatch(true, 20, 5)).toEqual({ max_upload_mb: 20 })
    expect(uploadPatch(true, null, 5)).toEqual({ max_upload_mb: null })
  })
})
