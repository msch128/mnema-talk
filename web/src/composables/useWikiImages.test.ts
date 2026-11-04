import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { altFromName, useWikiImages } from './useWikiImages'

const upload = vi.hoisted(() => vi.fn())
vi.mock('../lib/wikiApi', () => ({ uploadWikiImage: upload }))

describe('altFromName', () => {
  it.each<[string, string]>([
    ['Site map.png', 'Site map'],
    ['archive.tar.gz', 'archive.tar'],
    ['we[ird]\nname.jpg', 'we ird name'],
    ['  spaced   out .gif', 'spaced out'],
    ['noextension', 'noextension'],
    [`${'a'.repeat(100)}.png`, 'a'.repeat(60)],
    ['.png', '']
  ])('%j becomes %j', (name, alt) => {
    expect(altFromName(name)).toBe(alt)
  })
})

describe('pictures without a text field in view', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    upload.mockReset()
  })

  it('adds the Markdown at the end of the text', async () => {
    upload.mockResolvedValue({ id: 'i', url: '/api/media/x', markdown: '', size: 1 })
    const content = ref('text')
    const { addFiles } = useWikiImages(ref(null), content)
    await addFiles([new File(['x'], 'Map.png', { type: 'image/png' })])
    expect(content.value).toBe('text![Map](/api/media/x)')
  })

  it('leaves a paste or drop without files to the browser', () => {
    const { onPaste, onDrop } = useWikiImages(ref(null), ref(''))
    const paste = new Event('paste', { cancelable: true }) as ClipboardEvent
    onPaste(paste)
    const drop = new Event('drop', { cancelable: true }) as DragEvent
    Object.defineProperty(drop, 'dataTransfer', { value: { files: [] } })
    onDrop(drop)
    expect(paste.defaultPrevented).toBe(false)
    expect(drop.defaultPrevented).toBe(false)
    expect(upload).not.toHaveBeenCalled()
  })
})
