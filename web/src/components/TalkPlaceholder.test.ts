import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { setLocale } from '../i18n'
import { useVoiceStore } from '../stores/voice'
import TalkPlaceholder from './TalkPlaceholder.vue'

describe('TalkPlaceholder', () => {
  beforeEach(() => {
    localStorage.clear()
    setLocale('en')
    setActivePinia(createPinia())
  })

  it('asks to join an empty Talk that I am not in, and says so when joining is not possible yet', async () => {
    const w = mount(TalkPlaceholder, { props: { variant: 'empty', connected: false, canJoin: true } })
    expect(w.find('[data-testid="talk-empty"]').exists()).toBe(true)
    await w.find('button').trigger('click')
    expect(w.emitted('join')).toHaveLength(1)

    await w.setProps({ canJoin: false })
    expect(w.find('button').attributes('disabled')).toBeDefined()
    await w.setProps({ connected: true })
    expect(w.find('button').exists()).toBe(false)
  })

  it('offers all participants again when everyone without video is hidden', async () => {
    const voice = useVoiceStore()
    voice.setHideNoVideo(true)
    const w = mount(TalkPlaceholder, { props: { variant: 'no-video', connected: true, canJoin: true } })
    expect(w.find('[data-testid="no-video-hint"]').exists()).toBe(true)
    await w.find('button').trigger('click')
    expect(voice.hideNoVideo).toBe(false)
  })
})
