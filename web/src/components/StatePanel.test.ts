import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { Inbox } from '@lucide/vue'
import StatePanel from './StatePanel.vue'
import { i18nPlugin, setLocale } from '../i18n'

beforeEach(() => setLocale('en'))
type PanelProps = InstanceType<typeof StatePanel>['$props']
const mountPanel = (props: PanelProps, slots: Record<string, string> = {}) => mount(StatePanel, { props, slots, global: { plugins: [i18nPlugin] } })

describe('StatePanel', () => {
  it('shows a spinner with a hidden label while loading', () => {
    const w = mountPanel({ state: 'loading' })
    expect(w.attributes('role')).toBe('status')
    expect(w.find('.animate-spin').exists()).toBe(true)
    expect(w.get('.sr-only').text()).toBe('Loading…')
  })

  it('says one calm line, with an icon, when empty', () => {
    const w = mountPanel({ state: 'empty', icon: Inbox }, { default: 'Nothing here.' })
    expect(w.attributes('role')).toBeUndefined()
    expect(w.text()).toBe('Nothing here.')
    expect(w.findComponent(Inbox).exists()).toBe(true)
  })

  it('announces an error and offers a retry only when asked to', async () => {
    const plain = mountPanel({ state: 'error' }, { default: 'Failed.' })
    expect(plain.attributes('role')).toBe('alert')
    expect(plain.find('[data-testid="state-retry"]').exists()).toBe(false)

    const retryable = mountPanel({ state: 'error', retryable: true }, { default: 'Failed.' })
    expect(retryable.get('[data-testid="state-retry"]').text()).toBe('Try again')
    await retryable.get('[data-testid="state-retry"]').trigger('click')
    expect(retryable.emitted('retry')).toHaveLength(1)
  })

  it('has room for an extra action', () => {
    const w = mountPanel({ state: 'empty' }, { default: 'Empty.', action: '<button id="extra">Add</button>' })
    expect(w.find('#extra').exists()).toBe(true)
  })
})
