import { beforeEach, describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import { isDeletedUsername, shownName } from './deletedUser'

describe('deleted accounts', () => {
  beforeEach(() => setLocale('en'))

  it('recognises the placeholder name', () => {
    expect(isDeletedUsername('deleted-1a2b3c4d5e6f')).toBe(true)
    expect(isDeletedUsername('alice')).toBe(false)
    expect(isDeletedUsername(undefined)).toBe(false)
  })

  it('shows "Deleted user" in the member\'s language, and normal names as before', () => {
    expect(shownName({ username: 'deleted-1a2b3c4d5e6f', display_name: 'Deleted user' })).toBe('Deleted user')
    setLocale('de')
    expect(shownName({ username: 'deleted-1a2b3c4d5e6f', display_name: 'Deleted user' })).toBe('Gelöschter Nutzer')
    expect(shownName({ username: 'max', display_name: 'Max' })).toBe('Max')
    expect(shownName({ username: 'max', display_name: '' })).toBe('max')
    expect(shownName({})).toBe('')
  })
})
