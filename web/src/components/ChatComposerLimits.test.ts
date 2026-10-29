import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ChatComposer from './ChatComposer.vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { setLocale } from '../i18n'
import { channelFixture, fixtureId } from '../test-fixtures.fixture'

enableAutoUnmount(afterEach)

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useChatStore().activeChannel = channelFixture({ id: fixtureId(5), name: 'general', type: 'text' })
})

const mountComposer = (props: Record<string, unknown> = {}) => mount(ChatComposer, {
  props: { modelValue: 'good morning', canSend: true, canAttach: true, ...props },
  global: { directives: { tooltip: () => undefined } }
})

describe('the composer tells its limits up front', () => {
  it('shows nothing and sends while nothing limits the member', () => {
    const w = mountComposer()
    expect(w.find('[data-testid="composer-limit"]').exists()).toBe(false)
    expect(w.get('button.btn-primary').attributes('disabled')).toBeUndefined()
  })

  it('shows the wait of slow mode and holds the send button', () => {
    const w = mountComposer({ limit: { text: 'Slow mode: you can write again in 0:10', blocks: true, tone: 'calm' } })
    expect(w.get('[data-testid="composer-limit"]').text()).toBe('Slow mode: you can write again in 0:10')
    expect(w.get('button.btn-primary').attributes('disabled')).toBeDefined()
  })

  it('shows a counter that does not hold the send button', () => {
    const w = mountComposer({ limit: { text: '100 characters left', blocks: false, tone: 'calm' } })
    expect(w.get('[data-testid="composer-limit"]').text()).toBe('100 characters left')
    expect(w.get('button.btn-primary').attributes('disabled')).toBeUndefined()
  })

  it('names the upload limit on the attach button before a file is picked', () => {
    usePermissionsStore().info = { owner: false, permissions: 0, max_upload_mb: 25, role_ids: [], top_position: 0, catalog: [], channels: [] }
    const w = mountComposer()
    expect(w.get('[data-testid="composer-plus"]').attributes('aria-label')).toBe('Upload a file or image (up to 25 MB)')
  })
})
