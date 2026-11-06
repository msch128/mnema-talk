import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { defineComponent, h, ref } from 'vue'
import AudioInputSection from './AudioInputSection.vue'
import { useDialog } from '../composables/useDialog'
import { useVoiceStore } from '../stores/voice'
import { setLocale } from '../i18n'

beforeEach(() => { setActivePinia(createPinia()); setLocale('en') })
afterEach(() => { document.body.innerHTML = '' })

describe('push-to-talk key recording inside a dialog', () => {
  it('Escape cancels the recording and leaves the dialog open', async () => {
    const close = vi.fn()
    const Host = defineComponent({
      setup() {
        const root = ref<HTMLElement | null>(null)
        useDialog(root, { onClose: close })
        return () => h('div', { ref: root, tabindex: -1 }, [h(AudioInputSection)])
      }
    })
    const voice = useVoiceStore()
    voice.inputMode = 'ptt'
    voice.pttKey = 'KeyV'
    const w = mount(Host, { attachTo: document.body })
    const button = w.find('[data-testid="ptt-key-button"]')
    await button.trigger('click')
    expect(button.text()).toBe('Press a key…')
    ;(button.element as HTMLElement).dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', bubbles: true, cancelable: true }))
    await w.vm.$nextTick()
    expect(button.text()).not.toBe('Press a key…')
    expect(voice.pttKey).toBe('KeyV')
    expect(close).not.toHaveBeenCalled()
    w.unmount()
  })
})
