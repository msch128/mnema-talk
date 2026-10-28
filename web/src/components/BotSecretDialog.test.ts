import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import BotSecretDialog from './BotSecretDialog.vue'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('BotSecretDialog', () => {
  const mountDialog = () => mount(BotSecretDialog, { props: { secret: 'mb_secret123', botName: 'Echo' } })

  it('shows the secret once with a warning and a curl example', () => {
    const w = mountDialog()
    expect(w.get('[data-testid="bot-secret-value"]').text()).toBe('mb_secret123')
    expect(w.text()).toContain('cannot be shown again')
    expect(w.get('[data-testid="bot-secret-curl"]').text()).toBe(`curl -H "Authorization: Bot <token>" ${window.location.origin}/api/bot/v1/users/@me`)
    expect(w.text()).not.toContain('mb_secret123 ')
  })

  it('copies the secret and reports success or failure', async () => {
    const write = vi.fn().mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('denied'))
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: write }, configurable: true })
    const w = mountDialog()
    await w.get('[data-testid="bot-secret-copy"]').trigger('click')
    await vi.waitFor(() => expect(useToastStore().toasts.map(t => t.text)).toContain('Token copied.'))
    await w.get('[data-testid="bot-secret-copy"]').trigger('click')
    await vi.waitFor(() => expect(write).toHaveBeenCalledTimes(2))
    expect(write).toHaveBeenCalledWith('mb_secret123')
  })

  it('keeps the show-once token when the scrim is clicked', async () => {
    const w = mount(BotSecretDialog, { props: { secret: 'mb_secret123', botName: 'Echo' }, attachTo: document.body })
    const scrim = w.get('.dialog-scrim')
    await scrim.trigger('pointerdown')
    await scrim.trigger('pointerup')
    expect(w.emitted('close')).toBeUndefined()
    w.unmount()
  })

  it('closes from the done button', async () => {
    const w = mountDialog()
    await w.get('[data-testid="bot-secret-done"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('closes from the dialog header', async () => {
    const w = mountDialog()
    await w.get('[data-dialog-close]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
  })
})
