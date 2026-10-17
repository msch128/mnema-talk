import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ScreenLayerMenu from './ScreenLayerMenu.vue'
import ContextMenu from './ContextMenu.vue'
import { useVoiceStore } from '../stores/voice'
import { t } from '../i18n'

enableAutoUnmount(afterEach)

const layers = [{ width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 30 }, { width: 640, height: 360, fps: 30 }]

function watching() {
  const voice = useVoiceStore()
  voice.setChannel('v1')
  voice.setSubscriptionSink(() => {})
  voice.handleMediaState({ channel_id: 'v1', user_id: 'anna', screen: true, camera: false })
  voice.watchScreen('anna')
  return voice
}

const mountMenu = (props: Record<string, unknown> = {}) => mount(ScreenLayerMenu, {
  props: { userId: 'anna', ...props },
  global: { directives: { tooltip: {} } },
  attachTo: document.body,
})

beforeEach(() => setActivePinia(createPinia()))

describe('ScreenLayerMenu', () => {
  it('shows no button for a share without layers to choose from', async () => {
    const voice = watching()
    const wrapper = mountMenu()
    expect(wrapper.find('button').exists()).toBe(false)
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers: layers.slice(0, 1) })
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    await flushPromises()
    expect(wrapper.get('[data-testid="screen-layer-button"]').attributes('aria-label')).toBe(`${t('talk.layers.title')}: ${t('talk.layers.auto')}`)
    expect(wrapper.get('[data-testid="screen-layer-label"]').text()).toBe(t('talk.layers.auto'))
  })

  it('opens the menu, picks a layer and follows the choice', async () => {
    const voice = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    const wrapper = mountMenu()
    const button = wrapper.get('button')
    await button.trigger('pointerdown')
    await button.trigger('click')
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(wrapper.emitted('open-change')).toEqual([[true]])
    const menu = document.body.querySelector('[role="menu"]')
    expect(menu?.textContent).toContain('720p')
    const item = document.body.querySelector<HTMLElement>('[data-menu-item="layer-2"]')
    item?.click()
    await flushPromises()
    expect(voice.screenChoice).toEqual({ anna: 2 })
    expect(wrapper.get('[data-testid="screen-layer-label"]').text()).toBe('360p')
    // The menu stays open for the next pick; the button closes it.
    expect(button.attributes('aria-expanded')).toBe('true')
    await button.trigger('pointerdown')
    await button.trigger('click')
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(wrapper.emitted('open-change')).toEqual([[true], [false]])
  })

  it('follows the menu closing itself, e.g. on Escape or a click outside', async () => {
    const voice = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    const wrapper = mountMenu()
    const button = wrapper.get('button')
    await button.trigger('click')
    await flushPromises()
    wrapper.findComponent(ContextMenu).vm.$emit('update:modelValue', false)
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(wrapper.emitted('open-change')).toEqual([[true], [false]])
  })

  it('closes with a click that follows no pointer press, and reopens', async () => {
    const voice = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    const wrapper = mountMenu()
    const button = wrapper.get('button')
    await button.trigger('click')
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('true')
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
    await button.trigger('click')
    await flushPromises()
    expect(button.attributes('aria-expanded')).toBe('true')
  })

  it('closes the menu when the share loses its layers', async () => {
    const voice = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    const wrapper = mountMenu()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(document.body.querySelector('[role="menu"]')).not.toBeNull()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers: [] })
    await flushPromises()
    expect(document.body.querySelector('[role="menu"]')).toBeNull()
  })

  it('can hide the label and attach the menu to the full-screen stage', async () => {
    const voice = watching()
    voice.handleScreenLayers({ channel_id: 'v1', user_id: 'anna', layers })
    const host = document.createElement('div')
    document.body.append(host)
    const wrapper = mountMenu({ host, showLabel: false })
    expect(wrapper.find('[data-testid="screen-layer-label"]').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(host.querySelector('[role="menu"]')).not.toBeNull()
    expect(wrapper.findComponent(ContextMenu).props('teleportTo')).toBe(host)
    host.remove()
  })
})
