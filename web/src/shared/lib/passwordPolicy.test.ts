import { describe, expect, it } from 'vitest'
import { MIN_PASSWORD_LENGTH } from './passwordPolicy'

describe('runtime edges of pure chat helpers', () => {
  it('shares the documented minimum password length', () => { expect(MIN_PASSWORD_LENGTH).toBe(10) })
})
