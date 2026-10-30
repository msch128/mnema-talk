import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { ref } from 'vue'
import { useScreenLayerMenu } from './useScreenLayerMenu'
import { useVoiceStore } from '../stores/voice'
import type { ContextMenuItem } from '../components/menuTypes'
import { t } from '../i18n'

const layers = [{ width: 3840, height: 2160, fps: 60 }, { width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 30 }]

function watching() {
  const voice = useVoiceStore()
  voice.setChannel('v1')
  const sent: unknown[] = []
  voice.setSubscriptionSink(msg => { sent.push(msg) })
  voice.handleMediaState({ channel_id: 'v1', user_id: 'anna', screen: true, camera: false })
  voice.watchScreen('anna')
  voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
  sent.length = 0
  return { voice, sent }
}

const radios = (items: ContextMenuItem[]) => items.filter((i): i is Extract<ContextMenuItem, { type: 'radio' | 'checkbox' }> => i.type === 'radio')

beforeEach(() => setActivePinia(createPinia()))

describe('the viewer quality menu', () => {
  it('has nothing to choose from without at least two layers', () => {
    const { voice } = watching()
    const who = ref<string | null>('anna')
    const menu = useScreenLayerMenu(() => who.value)
    expect(menu.available.value).toBe(true)
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers: layers.slice(0, 1) })
    expect(menu.available.value).toBe(false)
    expect(menu.layers.value).toHaveLength(1)
    who.value = null
    expect(menu.available.value).toBe(false)
    expect(menu.layers.value).toEqual([])
    expect(menu.choice.value).toBe('auto')
    expect(menu.received.value).toBeUndefined()
  })

  it('lists Auto and each layer with what it really is', () => {
    watching()
    const menu = useScreenLayerMenu(() => 'anna')
    const items = menu.menuItems()
    expect(items[0]).toMatchObject({ type: 'label' })
    expect(radios(items).map(i => [i.id, i.label, i.subtitle, i.checked])).toEqual([
      ['layer-auto', t('talk.layers.auto'), t('talk.layers.autoHint'), true],
      ['layer-0', t('talk.layers.source'), '3840×2160 · 60 fps', false],
      ['layer-1', '1080p', '1920×1080 · 30 fps', false],
      ['layer-2', '720p', '1280×720 · 30 fps', false],
    ])
    expect(menu.buttonLabel.value).toBe(t('talk.layers.auto'))
  })

  it('says what automatic gives right now', () => {
    const { voice } = watching()
    const menu = useScreenLayerMenu(() => 'anna')
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 2, auto: true })
    expect(radios(menu.menuItems())[0]?.subtitle).toBe(t('talk.layers.autoNow', { name: '720p' }))
    // Before the first picture there is nothing to say yet.
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: -1, auto: true })
    expect(radios(menu.menuItems())[0]?.subtitle).toBe(t('talk.layers.autoHint'))
  })

  it('names what Auto chose on the button, "Auto (1080p)"', () => {
    const { voice } = watching()
    const menu = useScreenLayerMenu(() => 'anna')
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 1, auto: true })
    expect(menu.buttonLabel.value).toBe(t('talk.layers.autoWith', { name: '1080p' }))
    // The source is named by what it is, not by "Source".
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 0, auto: true })
    expect(menu.buttonLabel.value).toBe(t('talk.layers.autoWith', { name: '2160p60' }))
    expect(radios(menu.menuItems())[0]?.subtitle).toBe(t('talk.layers.autoNow', { name: '2160p60' }))
    // A chosen layer is just named.
    voice.setScreenChoice('anna', 1)
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 1, auto: false })
    expect(menu.buttonLabel.value).toBe('1080p')
  })

  it('applies a choice, labels the button with it and marks the layer', () => {
    const { voice, sent } = watching()
    const menu = useScreenLayerMenu(() => 'anna')
    radios(menu.menuItems())[3]?.action?.()
    expect(sent).toEqual([{ kind: 'screen', user_id: 'anna', on: true, quality: 2 }])
    expect(voice.screenChoice).toEqual({ anna: 2 })
    expect(menu.choice.value).toBe(2)
    expect(radios(menu.menuItems()).map(i => i.checked)).toEqual([false, false, false, true])
    expect(menu.buttonLabel.value).toBe('720p')

    radios(menu.menuItems())[1]?.action?.()
    expect(menu.buttonLabel.value).toBe(t('talk.layers.source'))
    radios(menu.menuItems())[0]?.action?.()
    expect(sent.at(-1)).toEqual({ kind: 'screen', user_id: 'anna', on: true, quality: 'auto' })
    expect(menu.choice.value).toBe('auto')
  })

  it('shows the layer actually received while a switch is under way', () => {
    const { voice } = watching()
    const menu = useScreenLayerMenu(() => 'anna')
    menu.menuItems()
    voice.setScreenChoice('anna', 2)
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 0, auto: false })
    expect(menu.buttonLabel.value).toBe(t('talk.layers.source'))
    const items = menu.menuItems()
    expect(items.at(-1)).toMatchObject({ type: 'label', label: t('talk.layers.switching') })
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: 2, auto: false })
    expect(menu.buttonLabel.value).toBe('720p')
    expect(menu.menuItems().some(i => i.type === 'label' && i.label === t('talk.layers.switching'))).toBe(false)
    // Before the first picture the chosen layer names the button.
    voice.handleScreenQuality({ channel_id: 'v1', user_id: 'anna', layer: -1, auto: false })
    expect(menu.buttonLabel.value).toBe('720p')
  })

  it('does not name a layer the share does not have', () => {
    const { voice } = watching()
    const menu = useScreenLayerMenu(() => 'anna')
    voice.screenChoice = { anna: 7 }
    expect(menu.buttonLabel.value).toBe('')
  })
})
