import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ConnectionStatsModal from './ConnectionStatsModal.vue'
import { useVoiceStore } from '../stores/voice'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))

describe('ConnectionStatsModal stable ping histories', () => {
  it.each([
    { history: [0, 0], points: '0.0,46.0 280.0,46.0' },
    { history: [10, 10], points: '0.0,29.2 280.0,29.2' },
  ])('draws a finite horizontal line for $history', ({ history, points }) => {
    useVoiceStore().pingHistory = history
    const wrapper = mount(ConnectionStatsModal)
    expect(wrapper.get('polyline').attributes('points')).toBe(points)
  })
})
