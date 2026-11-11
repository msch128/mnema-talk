import { describe, expect, it } from 'vitest'
import { confirm, pendingConfirm } from './confirm'

describe('menu and confirmation edge cases', () => {
  it('cancels an earlier request and prevents stale resolution from clearing the replacement', async () => {
    const first = confirm({ title: 'First' })
    const stale = pendingConfirm.value
    expect(stale).not.toBeNull()
    const second = confirm({ title: 'Second', danger: true })
    await expect(first).resolves.toBe(false)
    stale?.resolve(true)
    expect(pendingConfirm.value?.title).toBe('Second')
    pendingConfirm.value?.resolve(true)
    await expect(second).resolves.toBe(true)
    expect(pendingConfirm.value).toBeNull()
  })
})
