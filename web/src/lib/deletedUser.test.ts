import { beforeEach, describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import { isDeletedUsername, isRemovedMessage, shownName } from './deletedUser'

describe('deleted accounts', () => {
  beforeEach(() => setLocale('en'))

  it('recognises the placeholder name', () => {
    expect(isDeletedUsername('deleted-1a2b3c4d5e6f')).toBe(true)
    expect(isDeletedUsername('alice')).toBe(false)
    expect(isDeletedUsername(undefined)).toBe(false)
  })

  it('shows "Deleted member" in the member\'s language, and normal names as before', () => {
    expect(shownName({ username: 'deleted-1a2b3c4d5e6f', display_name: 'Deleted user' })).toBe('Deleted member')
    setLocale('de')
    expect(shownName({ username: 'deleted-1a2b3c4d5e6f', display_name: 'Deleted user' })).toBe('Gelöschtes Mitglied')
    expect(shownName({ username: 'max', display_name: 'Max' })).toBe('Max')
    expect(shownName({ username: 'max', display_name: '' })).toBe('max')
    expect(shownName({})).toBe('')
  })
})

describe('messages left behind by a deleted account', () => {
  it('count as removed only when nothing of them is left to show', () => {
    const gone = { username: 'deleted-1a2b3c4d5e6f', content: '' }
    expect(isRemovedMessage(gone)).toBe(true)
    expect(isRemovedMessage({ ...gone, attachments: [{}] })).toBe(false)
    expect(isRemovedMessage({ ...gone, embeds: [{}] })).toBe(false)
    expect(isRemovedMessage({ ...gone, poll: {} })).toBe(false)
    expect(isRemovedMessage({ ...gone, content: 'still here' })).toBe(false)
    expect(isRemovedMessage({ username: 'max', content: '' })).toBe(false)
  })
})
