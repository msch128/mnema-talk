import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import PendingMessages from './PendingMessages.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { setLocale, t } from '../i18n'
import type { PendingMessage } from '../stores/chat/outboxTypes'
import { userFixture } from '../test-fixtures.fixture'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  useAuthStore().user = userFixture({ id: 'me' })
})

function pending(status: 'waiting' | 'failed' | 'sending' = 'waiting', error = '', extra: Partial<PendingMessage> = {}) {
  const chat = useChatStore()
  chat.outbox = [{
    nonce: 'n1', channelId: 'c1', userId: 'me', parentId: null, replyToId: null, content: 'hello', createdAt: '2026-01-01T12:00:00Z', status, error,
    cause: 'offline', attempts: 0, possiblyDelivered: false, notBefore: 0, ...extra
  }]
  return mount(PendingMessages, { props: { channelId: 'c1' }, global: { directives: { tooltip: () => undefined } } })
}

describe('a message that has not been sent yet', () => {
  it('shows calmly as waiting, never in the error colour', () => {
    const view = pending()
    expect(view.text()).toContain('hello')
    expect(view.text()).toContain(t('outbox.waiting'))
    expect(view.find('.text-mnema-danger').exists()).toBe(false)
  })

  it('says it will retry, not that it waits for the connection, when the server was the problem', () => {
    const view = pending('waiting', '', { cause: 'server' })
    expect(view.text()).toContain(t('outbox.retrying'))
    expect(view.text()).not.toContain(t('outbox.waiting'))
  })

  it('shows the reason in the error colour only once the server refused it, with a retry', () => {
    const view = pending('failed', 'missing permission')
    expect(view.get('.text-mnema-danger').text()).toContain('missing permission')
    expect(view.text()).toContain(t('outbox.retry'))
  })

  it('announces all pending messages in one live region instead of one per row', () => {
    const chat = useChatStore()
    const view = pending()
    chat.outbox = [...chat.outbox, { ...chat.outbox[0]!, nonce: 'n2' }, { ...chat.outbox[0]!, nonce: 'n3', status: 'failed', error: 'x' }]
    return view.vm.$nextTick().then(() => {
      expect(view.findAll('[role="status"], [role="alert"]')).toHaveLength(1)
      expect(view.get('[role="status"]').text()).toBe(`${t('outbox.summaryWaitingMany', { count: 2 })}. ${t('outbox.summaryFailed', { count: 1 })}`)
    })
  })

  it('names the message in the buttons, so a list of them is not "Edit, Discard, Edit, Discard"', () => {
    const view = pending('failed', 'no', { content: 'x'.repeat(60) })
    const labels = view.findAll('button').map(b => b.attributes('aria-label'))
    const excerpt = `${'x'.repeat(40)}…`
    expect(labels).toEqual([t('outbox.retryFor', { excerpt }), t('outbox.editFor', { excerpt }), t('outbox.discardFor', { excerpt })])
  })

  it('offers only discarding once a try may have arrived, with a calm hint', () => {
    const view = pending('waiting', '', { possiblyDelivered: true })
    expect(view.text()).not.toContain(t('outbox.edit'))
    expect(view.text()).toContain(t('outbox.noEdit'))
    expect(view.text()).toContain(t('outbox.cancel'))
  })

  it('does not offer to edit or discard while the request is out', () => {
    const view = pending('sending')
    expect(view.text()).toContain(t('outbox.sending'))
    expect(view.text()).not.toContain(t('outbox.edit'))
    expect(view.text()).not.toContain(t('outbox.cancel'))
  })

  it('lets the member discard it', async () => {
    const view = pending()
    await view.findAll('button').find(b => b.text() === t('outbox.cancel'))!.trigger('click')
    expect(useChatStore().outbox).toEqual([])
    expect(view.find('[data-testid="pending-message"]').exists()).toBe(false)
  })

  it('lets the member change the text before it is sent', async () => {
    const view = pending()
    await view.findAll('button').find(b => b.text() === t('outbox.edit'))!.trigger('click')
    const editor = view.get('[data-testid="message-editor"]')
    await editor.setValue('hello, fixed')
    await editor.trigger('keydown.enter')
    await flushPromises()
    expect(useChatStore().outbox[0]?.content).toBe('hello, fixed')
    expect(view.text()).toContain('hello, fixed')
  })

  it('only shows the messages of its own conversation and thread', () => {
    const chat = useChatStore()
    const view = pending()
    chat.outbox = [...chat.outbox, { ...chat.outbox[0]!, nonce: 'n2', parentId: 'root' }, { ...chat.outbox[0]!, nonce: 'n3', channelId: 'c2' }]
    return view.vm.$nextTick().then(() => expect(view.findAll('[data-testid="pending-message"]')).toHaveLength(1))
  })
})
