import { describe, it, expect, beforeEach } from 'vitest'
import { defineComponent, h, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ImageChange } from './useStagedImage'
import { normaliseImage, useStagedImage } from './useStagedImage'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

function setup(saved = '/saved.png') {
  const value = ref<ImageChange>(null)
  const savedUrl = ref(saved)
  let staged!: ReturnType<typeof useStagedImage>
  const w = mount(defineComponent({
    setup() {
      staged = useStagedImage({ get: () => value.value, set: v => { value.value = v }, savedUrl: () => savedUrl.value })
      return () => h('div')
    }
  }))
  return { value, savedUrl, staged: () => staged, w }
}

function pickEvent(files: File[]) {
  const input = document.createElement('input')
  input.type = 'file'
  Object.defineProperty(input, 'files', { value: files, configurable: true })
  return { target: input } as unknown as Event
}

describe('useStagedImage', () => {
  it('shows the saved image until something is staged', () => {
    const { staged } = setup()
    expect(staged().previewUrl.value).toBe('/saved.png')
  })

  it('previews a picked file without writing anything', async () => {
    const { staged, value } = setup()
    staged().pick(pickEvent([new File(['x'], 'a.png', { type: 'image/png' })]))
    await nextTick()
    expect(value.value).toBeInstanceOf(File)
    expect(staged().previewUrl.value).toMatch(/^blob:/)
  })

  it('refuses a file over the limit with a toast and stages nothing', () => {
    const { staged, value } = setup()
    staged().pick(pickEvent([new File([new Uint8Array(6 * 1024 * 1024)], 'big.png')]))
    expect(value.value).toBeNull()
    expect(useToastStore().toasts[0]?.text).toContain('5 MB')
  })

  it('marks a saved image for removal, but a picked file with nothing saved is simply no change', async () => {
    const withSaved = setup()
    withSaved.staged().remove()
    expect(withSaved.value.value).toBe('remove')
    expect(withSaved.staged().previewUrl.value).toBe('')

    const without = setup('')
    without.value.value = new File(['x'], 'a.png')
    await nextTick()
    without.staged().remove()
    expect(without.value.value).toBeNull()
  })

  it('compares staged images by what they are', () => {
    const file = new File(['x'], 'a.png', { lastModified: 1 })
    expect(normaliseImage(file)).toBe(normaliseImage(new File(['y'], 'a.png', { lastModified: 1 })))
    expect(normaliseImage(file)).not.toBe(normaliseImage(new File(['xy'], 'a.png', { lastModified: 1 })))
    expect(normaliseImage(null)).toBeNull()
    expect(normaliseImage('remove')).toBe('remove')
  })
})
