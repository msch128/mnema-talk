import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { effectScope, nextTick } from 'vue'
import { usePttHint } from './usePttHint'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import { settingsTab } from '../lib/settingsDialog'

function start() {
  const scope = effectScope()
  scope.run(() => usePttHint())
  return scope
}

describe('usePttHint', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('says once what the key can do when a Talk starts with push to talk on', async () => {
    const voice = useVoiceStore()
    const toasts = useToastStore()
    voice.inputMode = 'ptt'
    voice.pttKey = 'F8'
    const scope = start()
    expect(toasts.toasts).toHaveLength(0)
    voice.isConnected = true
    await nextTick()
    expect(toasts.toasts).toHaveLength(1)
    expect(toasts.toasts[0]?.text).toBe(t('audio.pttFirstUse', { key: 'F8' }))
    voice.isConnected = false
    await nextTick()
    voice.isConnected = true
    await nextTick()
    expect(toasts.toasts).toHaveLength(1)
    scope.stop()
  })

  it('stays quiet for voice activity', async () => {
    const voice = useVoiceStore()
    const toasts = useToastStore()
    const scope = start()
    voice.isConnected = true
    await nextTick()
    expect(toasts.toasts).toHaveLength(0)
    scope.stop()
  })

  it('offers to choose a key when there is none', async () => {
    const voice = useVoiceStore()
    const toasts = useToastStore()
    voice.inputMode = 'ptt'
    voice.pttKey = ''
    const scope = start()
    voice.isConnected = true
    await nextTick()
    const toast = toasts.toasts[0]
    expect(toast?.action?.label).toBe(t('audio.pttFirstUseAction'))
    await toast?.action?.onClick()
    expect(settingsTab.value).toBe('audio')
    scope.stop()
  })
})
