import { describe, expect, it } from 'vitest'
import { ApiError } from '../shared/lib/api'
import { isStaleRevision } from './wikiErrors'

describe('a save the page changed under', () => {
  it('is told by the code of the refusal', () => {
    expect(isStaleRevision(new ApiError(409, 'STALE_REVISION', 'any wording at all'))).toBe(true)
  })

  it('is not told by its words or its status', () => {
    expect(isStaleRevision(new ApiError(409, 'CONFLICT', 'the page changed since you opened it'))).toBe(false)
    expect(isStaleRevision(new Error('STALE_REVISION'))).toBe(false)
    expect(isStaleRevision(null)).toBe(false)
  })
})
