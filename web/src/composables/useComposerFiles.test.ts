import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useComposerFiles } from './useComposerFiles'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

const png = new File(['x'], 'a.png', { type: 'image/png' })
const pdf = new File(['x'], 'c.pdf', { type: 'application/pdf' })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

function setup(opts: { canAttach?: boolean; busy?: boolean } = {}) {
  const staged: File[][] = []
  const files = useComposerFiles({ add: f => staged.push([...f]), canAttach: () => opts.canAttach ?? true, busy: () => opts.busy ?? false })
  return { files, staged, toasts: useToastStore() }
}

function pasteEvent(files: File[], data: Record<string, string> = {}) {
  const e = new Event('paste', { cancelable: true }) as ClipboardEvent
  Object.defineProperty(e, 'clipboardData', { value: { files, types: [], getData: (k: string) => data[k] ?? '' } })
  return e
}

describe('useComposerFiles', () => {
  it('stages a pasted image and sends nothing', () => {
    const { files, staged } = setup()
    const e = pasteEvent([png])
    files.onPaste(e)
    expect(e.defaultPrevented).toBe(true)
    expect(staged).toEqual([[png]])
  })

  it('leaves a text paste to the textarea', () => {
    const { files, staged } = setup()
    const e = pasteEvent([], { 'text/plain': 'hello' })
    files.onPaste(e)
    expect(e.defaultPrevented).toBe(false)
    expect(staged).toEqual([])
  })

  it('stages every dropped file, whatever its type', () => {
    const { files, staged, toasts } = setup()
    files.attachFiles([pdf, png])
    expect(staged).toEqual([[pdf, png]])
    expect(toasts.toasts).toEqual([])
  })

  it('stages the files picked with the button and clears the input', () => {
    const { files, staged } = setup()
    const input = document.createElement('input')
    input.type = 'file'
    Object.defineProperty(input, 'files', { value: [png, pdf] })
    files.onPick({ target: input } as unknown as Event)
    expect(staged).toEqual([[png, pdf]])
    expect(input.value).toBe('')
  })

  it('shows the permission message where attaching is not allowed', () => {
    const { files, staged, toasts } = setup({ canAttach: false })
    files.attachFiles([png])
    expect(staged).toEqual([])
    expect(toasts.toasts.at(-1)).toMatchObject({ type: 'error', text: 'You can’t attach files here.' })
  })

  it('ignores a drop while the message is being sent', () => {
    const { files, staged } = setup({ busy: true })
    files.attachFiles([png])
    expect(staged).toEqual([])
  })
})
