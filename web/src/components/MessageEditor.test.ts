import { requireValue } from '../test-fixtures.fixture'
import { describe, it, expect, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import MessageEditor from './MessageEditor.vue'

let w!: ReturnType<typeof mount<typeof MessageEditor>>
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

  it('forwards buttons, compact layout and exposed focus', async () => {
    w = mount(MessageEditor, { props: { compact: true }, attachTo: document.body })
    await flushPromises()
    expect(w.find('textarea').attributes('aria-label')).toBeUndefined()
    expect(w.find('textarea').classes()).toContain('p-1.5')
    await requireValue(w.findAll('button')[0]).trigger('click')
    await requireValue(w.findAll('button')[1]).trigger('click')
    expect(w.emitted('cancel')).toHaveLength(1)
    expect(w.emitted('save')).toHaveLength(1)
    w.find('textarea').element.blur()
    w.vm.focus()
    expect(document.activeElement).toBe(w.find('textarea').element)
  })

  it('tolerates removal before its deferred focus', async () => {
    const focus = vi.spyOn(HTMLTextAreaElement.prototype, 'focus')
    w = mount(MessageEditor)
    w.unmount()
    await flushPromises()
    expect(focus).not.toHaveBeenCalled()
    w.vm.focus()
    expect(focus).not.toHaveBeenCalled()
    focus.mockRestore()
  })

  it('updates the model and disables saving while busy', async () => {
    w = mount(MessageEditor, { props: { modelValue: 'x', saving: true, label: 'Edit message' } })
    await w.find('textarea').setValue('y')
    expect(w.emitted('update:modelValue')).toEqual([['y']])
    expect(w.find('textarea').attributes('aria-label')).toBe('Edit message')
    const save = w.findAll('button').at(-1)
    expect(requireValue(save).attributes('disabled')).toBeDefined()
  })
})
