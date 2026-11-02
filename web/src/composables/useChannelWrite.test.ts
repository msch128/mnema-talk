import { beforeEach, describe, expect, it } from 'vitest'
import { ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { useChannelWrite } from './useChannelWrite'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { PERMISSION_BITS } from '../lib/permissions'
import { channelFixture, fixtureId } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

const channel = channelFixture({ id: fixtureId(5), name: 'general', type: 'text' })
const noDm = { isDm: ref(false), canSend: ref(false), placeholder: ref('') }

function holds(permissions: number, timeoutUntil?: string, writeAt?: string) {
  usePermissionsStore().info = {
    owner: false, permissions: 0, max_upload_mb: 50, role_ids: [], top_position: 0, catalog: [],
    channels: [{ channel_id: channel.id, permissions }], ...(timeoutUntil ? { timeout_until: timeoutUntil } : {}),
    ...(writeAt ? { new_member: { write_at: writeAt } } : {})
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useChatStore().activeChannel = channel
})

describe('what the composer says about writing', () => {
  it('invites a member who may write', () => {
    holds(PERMISSION_BITS.view_channel | PERMISSION_BITS.send_messages)
    const write = useChannelWrite(noDm, () => false)
    expect(write.canSend.value).toBe(true)
    expect(write.placeholder.value).toBe('Message #general')
    expect(write.canAttach.value).toBe(false)
  })

  it('says a channel is read-only for a member without the permission', () => {
    holds(PERMISSION_BITS.view_channel)
    expect(useChannelWrite(noDm, () => false).placeholder.value).toBe('You can’t write in this channel.')
  })

  it('names a running timeout as the reason', () => {
    holds(PERMISSION_BITS.view_channel, new Date(Date.now() + 60_000).toISOString())
    expect(useChannelWrite(noDm, () => false).placeholder.value).toBe('You are timed out and can’t write right now.')
  })

  it('tells a new member when writing opens up instead of calling the channel read-only', () => {
    holds(PERMISSION_BITS.view_channel, undefined, new Date(Date.now() + 12 * 60_000).toISOString())
    expect(useChannelWrite(noDm, () => false).placeholder.value).toBe('You can write in 12 min.')
  })

  it('leaves a conversation to its own answer', () => {
    const dm = { isDm: ref(true), canSend: ref(false), placeholder: ref('No messages here') }
    const write = useChannelWrite(dm, () => false)
    expect(write.canSend.value).toBe(false)
    expect(write.placeholder.value).toBe('No messages here')
  })
})
