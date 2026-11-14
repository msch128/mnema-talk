import { describe, expect, it } from 'vitest'
import { memberLabel } from './mentionLabel'

const members = [
  { username: 'Bot-FCEAF0', display_name: 'Mnemo' },
  { username: 'max', display_name: 'Max' },
  { username: 'plain', display_name: '' },
  { username: '', display_name: 'Nobody' }
]

describe('memberLabel', () => {
  it('gives the display name of the member holding the username, whatever its case', () => {
    expect(memberLabel(members, 'bot-fceaf0')).toBe('Mnemo')
  })

  it('has nothing to add when the display name is the username or is missing', () => {
    expect(memberLabel(members, 'max')).toBeUndefined()
    expect(memberLabel(members, 'plain')).toBeUndefined()
  })

  it('knows nothing of a username no member holds', () => {
    expect(memberLabel(members, 'deleted')).toBeUndefined()
    expect(memberLabel([], 'max')).toBeUndefined()
  })
})
