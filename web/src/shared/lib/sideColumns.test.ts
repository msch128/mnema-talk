import { afterEach, describe, expect, it } from 'vitest'
import { memberListShown, membersOverlay, membersSteppedAside, toggleMemberList } from './sideColumns'

afterEach(() => { membersSteppedAside.value = false; membersOverlay.value = false })

describe('the member button', () => {
  it('shows and hides the member list as a column when there is room', () => {
    const chat = { showMemberList: false }
    toggleMemberList(chat)
    expect(chat.showMemberList).toBe(true)
    expect(memberListShown(chat.showMemberList)).toBe(true)
    expect(membersOverlay.value).toBe(false)
    toggleMemberList(chat)
    expect(chat.showMemberList).toBe(false)
  })

  it('is not pressed while the list stepped aside, and pressing it opens the list over the chat', () => {
    const chat = { showMemberList: true }
    membersSteppedAside.value = true
    expect(memberListShown(chat.showMemberList)).toBe(false)
    toggleMemberList(chat)
    expect(membersOverlay.value).toBe(true)
    expect(memberListShown(chat.showMemberList)).toBe(true)
    toggleMemberList(chat)
    expect(chat.showMemberList).toBe(false)
    expect(membersOverlay.value).toBe(false)
  })
})
