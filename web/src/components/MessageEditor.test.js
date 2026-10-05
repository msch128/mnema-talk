import { describe, it, expect, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import MessageEditor from './MessageEditor.vue'

let w
afterEach(() => {
  w?.unmount()
  document.body.innerHTML = ''
})

describe('MessageEditor', () => {
  it('takes focus with the cursor at the end when it opens', async () => {
    w = mount(MessageEditor, { props: { modelValue: 'hello' }, attachTo: document.body })
    await flushPromises()
    const ta = w.find('textarea').element
    expect(document.activeElement).toBe(ta)
    expect(ta.selectionStart).toBe(5)
  })

  it('Enter saves, Shift+Enter does not, Escape cancels', async () => {
    w = mount(MessageEditor, { props: { modelValue: 'x' }, attachTo: document.body })
    const ta = w.find('textarea')
    await ta.trigger('keydown', { key: 'Enter', shiftKey: true })
    expect(w.emitted('save')).toBeUndefined()
    await ta.trigger('keydown', { key: 'Enter' })
    expect(w.emitted('save')).toHaveLength(1)
    await ta.trigger('keydown', { key: 'Escape' })
    expect(w.emitted('cancel')).toHaveLength(1)
  })

  it('updates the model and disables saving while busy', async () => {
    w = mount(MessageEditor, { props: { modelValue: 'x', saving: true, label: 'Edit message' } })
    await w.find('textarea').setValue('y')
    expect(w.emitted('update:modelValue')).toEqual([['y']])
    expect(w.find('textarea').attributes('aria-label')).toBe('Edit message')
    const save = w.findAll('button').at(-1)
    expect(save.attributes('disabled')).toBeDefined()
  })
})
