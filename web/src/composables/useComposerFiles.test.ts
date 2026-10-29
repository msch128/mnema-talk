import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { ref } from 'vue'
import { useComposerFiles } from './useComposerFiles'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

const png = new File(['x'], 'a.png', { type: 'image/png' })
const mov = new File(['x'], 'b.mov', { type: 'video/mp4' })
const pdf = new File(['x'], 'c.pdf', { type: 'application/pdf' })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  // jsdom has no DataTransfer.
  vi.stubGlobal('DataTransfer', class {
    private list: File[] = []
    items = { add: (f: File) => { this.list.push(f) } }
    get files() { return this.list as unknown as FileList }
  })
})

function setup(opts: { canAttach?: boolean; busy?: boolean } = {}) {
  const input = document.createElement('input')
  input.type = 'file'
  const received: File[][] = []
  input.addEventListener('change', () => received.push(Array.from(input.files ?? [])))
  Object.defineProperty(input, 'files', { writable: true, value: null })
  const files = useComposerFiles({ fileInput: ref(input), canAttach: () => opts.canAttach ?? true, busy: () => opts.busy ?? false })
  return { files, received, toasts: useToastStore() }
}

function pasteEvent(files: File[], data: Record<string, string> = {}) {
  const e = new Event('paste', { cancelable: true }) as ClipboardEvent
  Object.defineProperty(e, 'clipboardData', { value: { files, types: [], getData: (k: string) => data[k] ?? '' } })
  return e
}

describe('useComposerFiles', () => {
  it('sends a pasted image through the file input', () => {
    const { files, received } = setup()
    const e = pasteEvent([png])
    files.onPaste(e)
    expect(e.defaultPrevented).toBe(true)
    expect(received).toEqual([[png]])
  })

  it('leaves a text paste to the textarea', () => {
    const { files, received } = setup()
    const e = pasteEvent([], { 'text/plain': 'hello' })
    files.onPaste(e)
    expect(e.defaultPrevented).toBe(false)
    expect(received).toEqual([])
  })

  it('attaches a dropped file and says that only the first was used', () => {
    const { files, received, toasts } = setup()
    files.attachFiles([mov, png])
    expect(received).toEqual([[mov]])
    expect(toasts.toasts.at(-1)?.text).toBe('A message carries one file. The first one was attached.')
  })

  it('shows the permission message where attaching is not allowed', () => {
    const { files, received, toasts } = setup({ canAttach: false })
    files.attachFiles([png])
    expect(received).toEqual([])
    expect(toasts.toasts.at(-1)).toMatchObject({ type: 'error', text: 'You can’t attach files here.' })
  })

  it('refuses files that are not images or videos', () => {
    const { files, received, toasts } = setup()
    files.attachFiles([pdf])
    expect(received).toEqual([])
    expect(toasts.toasts.at(-1)?.text).toBe('Only images and videos can be attached.')
  })

  it('ignores a drop while an upload runs', () => {
    const { files, received } = setup({ busy: true })
    files.attachFiles([png])
    expect(received).toEqual([])
  })
})
