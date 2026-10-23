import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { buildNotificationBody, messageMentionsMeDirectly } from './chat/notifications'
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
