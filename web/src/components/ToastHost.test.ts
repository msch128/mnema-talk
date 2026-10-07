import { requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import ToastHost from './ToastHost.vue'
import { useToastStore } from '../stores/toast'

beforeEach(() => setActivePinia(createPinia()))

describe('ToastHost', () => {
  it('renders toasts in an aria-live region; errors are alerts', async () => {
    const toasts = useToastStore()
    const w = mount(ToastHost)
    expect(w.attributes('aria-live')).toBe('polite')
    toasts.success('Link kopiert')
    toasts.error('Datei ist zu groß (max. 50 MB).', { detail: 'urlaub.mov hat 212 MB.' })
    await w.vm.$nextTick()
    const items = w.findAll('[data-toast]')
    expect(requireValue(items[0]).attributes('role')).toBe('status')
    expect(requireValue(items[1]).attributes('role')).toBe('alert')
    expect(requireValue(items[1]).text()).toContain('urlaub.mov')
    // errors can be closed
    await requireValue(items[1]).find('button').trigger('click')
    expect(w.findAll('[data-toast]')).toHaveLength(1)
  })

  it('runs a toast action once and dismisses the toast', async () => {
    const toasts = useToastStore()
    let called = 0
    toasts.info('Neue Version', { action: { label: 'Neu laden', onClick: () => { called++ } } })
    const w = mount(ToastHost)
    await w.find('[data-toast] button').trigger('click')
    expect(called).toBe(1)
    expect(toasts.toasts).toHaveLength(0)
  })

  it('keeps a persistent warning dismissible without running its action', async () => {
    const toasts = useToastStore()
    let muted = false
    toasts.info('Bei dieser Aufnahme ist kein Schutz vor erneut aufgenommenem Gesprächston bestätigt.', {
      action: { label: 'Stream-Audio stummschalten', onClick: () => { muted = true } },
    })
    const w = mount(ToastHost, { global: { mocks: { $t: () => 'Schließen' } } })
    expect(w.find('[role="status"]').text()).toContain('Gesprächston')
    const close = w.find('button[aria-label="Schließen"]')
    expect(close.exists()).toBe(true)
    await close.trigger('click')
    expect(muted).toBe(false)
    expect(toasts.toasts).toHaveLength(0)
  })
})

it('uses informational icon and tone for a malformed runtime toast type', () => {
  const toasts = useToastStore(); toasts.info('Recovered runtime notification')
  // A persisted or plugin-created runtime object can bypass the static union.
  Reflect.set(requireValue(toasts.toasts[0]), 'type', 'unexpected')
  const w = mount(ToastHost)
  expect(w.find('[data-toast] svg').classes()).toContain('text-mnema-mint')
  expect(w.find('[data-toast]').attributes('role')).toBe('status')
  w.unmount()
})
