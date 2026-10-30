import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { buildNotificationBody, messageMentionsMeDirectly, triggerBrowserNotification } from './chat/notifications'
import { platform } from '../lib/platform'
import { vi } from 'vitest'
import { notifyShowsText } from '../lib/notifyPreview'
import type { Cx } from './chat/state'
import { usePermissionsStore } from './permissions'
import { setLocale } from '../i18n'
import { messageFixture, userFixture } from '../test-fixtures.fixture'

beforeEach(() => { setLocale('en'); setActivePinia(createPinia()) })

describe('OS notification text', () => {
  it('reads like a reply preview: no Markdown, spoilers hidden, emoji as :name:', () => {
    const body = buildNotificationBody(messageFixture({ content: '**Look** at this ||secret plan|| <:blob:11111111-2222-4333-8444-555555555555>\n> quoted' }))
    expect(body).toBe('Look at this Spoiler :blob: quoted')
  })

  it('is cut to 140 characters', () => {
    expect(Array.from(buildNotificationBody(messageFixture({ content: 'x'.repeat(300) }))).length).toBe(140)
  })

  it('names attachments and stays empty without content', () => {
    const withFile = messageFixture({ content: '', attachments: [{ id: 'a', filename: 'f.png', url: '/x', mime_type: 'image/png', size: 1 }] as never })
    expect(buildNotificationBody(withFile)).toBe('Attachment')
    expect(buildNotificationBody(messageFixture({ content: '' }))).toBe('')
  })
})

describe('what counts as addressed to me', () => {
  const cx = { authStore: { user: userFixture({ id: 'me', username: 'max' }) } } as unknown as Cx

  it('counts my name, my role and a reply to me, but not @all', () => {
    usePermissionsStore().info = { owner: false, permissions: 0, max_upload_mb: 1, role_ids: ['r1'], top_position: 0, catalog: [], channels: [] }
    expect(messageMentionsMeDirectly(cx, messageFixture({ content: 'hi @max', mentions: ['me'] }))).toBe(true)
    expect(messageMentionsMeDirectly(cx, messageFixture({ content: 'ping @Team', mentions: ['me'], mention_roles: ['r1'] }))).toBe(true)
    expect(messageMentionsMeDirectly(cx, messageFixture({ content: 'ok', reply_to: { id: 'x', user_id: 'me', content: '', deleted: false } as never }))).toBe(true)
    expect(messageMentionsMeDirectly(cx, messageFixture({ content: '@all hi', mentions: ['me'] }))).toBe(false)
    expect(messageMentionsMeDirectly(cx, messageFixture({ content: 'ping', mention_roles: ['r2'] }))).toBe(false)
  })
})

describe('what an OS notification says and where its click leads', () => {
  const channel = { id: 'ch1', name: 'general' }
  const cx = {
    authStore: { user: userFixture({ id: 'me', username: 'max', presence: 'online' }) },
    cs: {
      allChannels: { value: [channel] }, activeChannel: { value: null }, followKnown: { value: false }, followedThreads: { value: new Set() },
      readStates: { value: {} }
    },
  } as unknown as Cx

  beforeEach(() => { notifyShowsText.value = true })

  function shown(msg: ReturnType<typeof messageFixture>) {
    const notify = vi.spyOn(platform, 'notify').mockImplementation(() => {})
    vi.spyOn(platform, 'notificationPermission').mockReturnValue('granted')
    triggerBrowserNotification(cx, msg, true)
    return notify.mock.calls[0]?.[0]
  }

  it('names who wrote and in which channel, and carries the message for the click', () => {
    const n = shown(messageFixture({ id: 'm1', channel_id: 'ch1', display_name: 'Alice', content: 'hello @max' }))
    expect(n).toMatchObject({ title: 'Alice (#general)', channelId: 'ch1', messageId: 'm1' })
    expect(n?.body).toContain('hello')
  })

  it('shows no message text when the member or the operator turned it off', () => {
    notifyShowsText.value = false
    const n = shown(messageFixture({ id: 'm3', channel_id: 'ch1', display_name: 'Alice', content: 'secret' }))
    expect(n?.body).not.toContain('secret')
  })

  it('says when it is a reply in a thread', () => {
    const n = shown(messageFixture({ id: 'm2', channel_id: 'ch1', parent_id: 'root', display_name: 'Alice', content: 'sure' }))
    expect(n?.title).toBe('Alice (#general, Thread)')
  })
})
