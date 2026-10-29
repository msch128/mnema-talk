import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import MemberRow from './MemberRow.vue'
import { useChatStore } from '../stores/chat'
import { userFixture } from '../test-fixtures.fixture'

describe('MemberRow', () => {
  it('opens the profile on Enter and on Space, like a button', async () => {
    setActivePinia(createPinia())
    const chat = useChatStore()
    const open = vi.spyOn(chat, 'openUserProfile').mockResolvedValue(undefined)
    const member = userFixture({ username: 'ada' })
    const w = mount(MemberRow, { props: { member } })
    const row = w.get('[role="button"]')
    await row.trigger('keydown', { key: 'Enter' })
    await row.trigger('keydown', { key: ' ' })
    expect(open).toHaveBeenCalledTimes(2)
  })
})
