import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h, nextTick, reactive } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { confirmDiscard, normalise, provideDraftScope, useSettingsDraft } from './useSettingsDraft'
import type { SettingsDraftOptions } from './useSettingsDraft'
import { pendingConfirm } from '../lib/confirm'
import { setLocale } from '../i18n'

const mounted: { unmount: () => void }[] = []
beforeEach(() => setLocale('en'))
afterEach(() => { for (const w of mounted.splice(0)) w.unmount() })

interface Form { name: string; limit: number | string; roles: string[]; lobby: boolean }

function setup(overrides: Partial<SettingsDraftOptions<Form>> = {}, start: Form = { name: 'Games', limit: 0, roles: [], lobby: false }) {
  const source = reactive<Form>({ ...start })
  let api!: ReturnType<typeof useSettingsDraft<Form>>
  const save = vi.fn(async () => {})
  const wrapper = mount(defineComponent({
    setup() {
      api = useSettingsDraft<Form>({
        source: () => source,
        normalisers: { name: normalise.text, limit: normalise.count, roles: normalise.set, lobby: normalise.flag },
        save,
        ...overrides
      })
      return () => h('div')
    }
  }))
  mounted.push(wrapper)
  return { api, source, save, wrapper }
}

describe('normalise', () => {
  it('treats equivalent values as equal', () => {
    expect(normalise.text('  Games ')).toBe('Games')
    expect(normalise.text(null)).toBe('')
    expect(normalise.text(undefined)).toBe('')
    expect([0, '', null, undefined, '0', NaN, -3, 'x'].map(normalise.count)).toEqual([0, 0, 0, 0, 0, 0, 0, 0])
    expect(normalise.count('7')).toBe(7)
    expect(normalise.count(7.9)).toBe(7)
    expect(normalise.set(['b', 'a', 'b'])).toEqual(['a', 'b'])
    expect(normalise.set(null)).toEqual([])
    expect(normalise.flag(undefined)).toBe(false)
  })
})

describe('useSettingsDraft', () => {
  it('starts clean and cannot save', () => {
    const { api } = setup()
    expect(api.dirty.value).toBe(false)
    expect(api.canSave.value).toBe(false)
  })

  it('detects changes field by field and ignores equivalent values', async () => {
    const { api } = setup({}, { name: 'Games', limit: 0, roles: ['a', 'b'], lobby: false })
    api.draft.name = '  Games  '
    api.draft.limit = ''
    api.draft.roles = ['b', 'a', 'a']
    expect(api.dirty.value).toBe(false)
    api.draft.limit = 4
    expect(api.changed.value).toEqual(['limit'])
    api.draft.roles = ['a']
    expect(api.changed.value).toEqual(['limit', 'roles'])
  })

  it('lets a normaliser look at the other fields', () => {
    const { api } = setup({ normalisers: { roles: (value, state) => (state.lobby ? normalise.set(value) : []) } })
    api.draft.roles = ['x']
    expect(api.dirty.value).toBe(false)
    api.draft.lobby = true
    expect(api.changed.value).toEqual(['roles', 'lobby'])
  })

  it('saves only when valid', () => {
    const { api } = setup({ validate: d => (normalise.text(d.name) ? {} : { name: 'required' }) })
    api.draft.name = ' '
    expect(api.dirty.value).toBe(true)
    expect(api.errors.value).toEqual({ name: 'required' })
    expect(api.canSave.value).toBe(false)
    api.draft.name = 'Other'
    expect(api.canSave.value).toBe(true)
  })

  it('stays off while blocked', () => {
    const { api } = setup({ blocked: () => true })
    api.draft.name = 'Other'
    expect(api.canSave.value).toBe(false)
  })

  it('passes only the changes to save and is clean afterwards', async () => {
    const { api, save } = setup()
    api.draft.limit = 5
    expect(await api.save()).toBe(true)
    const context = save.mock.calls[0] as unknown as [{ changes: Partial<Form>; changed: string[] }]
    expect(context[0].changes).toEqual({ limit: 5 })
    expect(context[0].changed).toEqual(['limit'])
    expect(api.dirty.value).toBe(false)
    expect(api.canSave.value).toBe(false)
  })

  it('does nothing when there is nothing to save', async () => {
    const { api, save } = setup()
    expect(await api.save()).toBe(false)
    expect(save).not.toHaveBeenCalled()
  })

  it('calls saved after success but not after a failure', async () => {
    const saved = vi.fn()
    const { api } = setup({ saved })
    api.draft.name = 'A'
    await api.save()
    expect(saved).toHaveBeenCalledTimes(1)
    const failing = setup({ saved: vi.fn(), save: async () => { throw new Error('nope') } })
    failing.api.draft.name = 'A'
    expect(await failing.api.save()).toBe(false)
    expect(failing.api.error.value).toBe('nope')
    expect(failing.api.dirty.value).toBe(true)
  })

  it('reports a failed save and keeps the draft', async () => {
    const { api } = setup({ save: async () => { throw new Error('server said no') } })
    api.draft.name = 'A'
    await api.save()
    expect(api.error.value).toBe('server said no')
    expect(api.draft.name).toBe('A')
    expect(api.saving.value).toBe(false)
    api.reset()
    expect(api.error.value).toBe('')
    expect(api.draft.name).toBe('Games')
  })

  it('leaves only the unsaved fields changed after a partial failure', async () => {
    const { api } = setup({
      save: async ({ commit }) => {
        commit(['name', 'limit'])
        throw new Error('second step failed')
      }
    })
    api.draft.name = 'A'
    api.draft.limit = 3
    api.draft.lobby = true
    await api.save()
    expect(api.changed.value).toEqual(['lobby'])
    expect(api.error.value).toContain('Some changes were saved')
    expect(api.error.value).toContain('second step failed')
  })

  it('does not run two saves at once', async () => {
    let release = () => {}
    const { api, save } = setup({ save: () => new Promise<void>(resolve => { release = resolve }) })
    api.draft.name = 'A'
    const first = api.save()
    await nextTick()
    expect(api.saving.value).toBe(true)
    expect(await api.save()).toBe(false)
    release()
    await first
    expect(save).not.toHaveBeenCalled()
  })

  it('resets every edited field to the saved state', () => {
    const { api } = setup({}, { name: 'Games', limit: 2, roles: ['a'], lobby: false })
    api.draft.name = 'B'
    api.draft.roles = ['z']
    api.draft.lobby = true
    api.reset()
    expect({ ...api.draft }).toEqual({ name: 'Games', limit: 2, roles: ['a'], lobby: false })
    expect(api.dirty.value).toBe(false)
  })

  it('follows a changed saved state for untouched fields and keeps edited ones', async () => {
    const { api, source } = setup()
    api.draft.limit = 9
    source.name = 'Renamed elsewhere'
    source.limit = 3
    await nextTick()
    expect(api.draft.name).toBe('Renamed elsewhere')
    expect(api.draft.limit).toBe(9)
    expect(api.changed.value).toEqual(['limit'])
    source.limit = 9
    await nextTick()
    expect(api.dirty.value).toBe(false)
  })

  it('saves on Ctrl+S and Cmd+S', async () => {
    const { api, save } = setup()
    api.draft.name = 'A'
    const event = new KeyboardEvent('keydown', { key: 's', ctrlKey: true, cancelable: true })
    api.onKeydown(event)
    expect(event.defaultPrevented).toBe(true)
    await flushPromises()
    expect(save).toHaveBeenCalledTimes(1)
    api.draft.name = 'B'
    api.onKeydown(new KeyboardEvent('keydown', { key: 'S', metaKey: true, cancelable: true }))
    await flushPromises()
    expect(save).toHaveBeenCalledTimes(2)
    const plain = new KeyboardEvent('keydown', { key: 's', cancelable: true })
    api.onKeydown(plain)
    expect(plain.defaultPrevented).toBe(false)
  })

  it('warns the browser about unsaved changes while mounted', () => {
    const { api, wrapper } = setup()
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
    api.draft.name = 'A'
    const dirtyEvent = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirtyEvent)
    expect(dirtyEvent.defaultPrevented).toBe(true)
    wrapper.unmount()
    const after = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(after)
    expect(after.defaultPrevented).toBe(false)
  })
})

describe('discard guard', () => {
  it('lets a clean draft go without asking', async () => {
    const { api } = setup()
    expect(await api.guard()).toBe(true)
    expect(pendingConfirm.value).toBeNull()
  })

  it('asks "Discard changes?" for a dirty draft and follows the answer', async () => {
    const { api } = setup()
    api.draft.name = 'A'
    const keep = api.guard()
    expect(pendingConfirm.value?.title).toBe('Discard changes?')
    pendingConfirm.value?.resolve(false)
    expect(await keep).toBe(false)
    const drop = confirmDiscard()
    pendingConfirm.value?.resolve(true)
    expect(await drop).toBe(true)
  })
})

describe('draft scope', () => {
  it('knows whether any draft inside it is unsaved', async () => {
    let scope!: ReturnType<typeof provideDraftScope>
    const apis: ReturnType<typeof useSettingsDraft<Form>>[] = []
    const Child = defineComponent({
      setup() {
        apis.push(useSettingsDraft<Form>({ source: () => ({ name: 'a', limit: 0, roles: [], lobby: false }), save: async () => {} }))
        return () => h('div')
      }
    })
    const wrapper = mount(defineComponent({
      setup() {
        scope = provideDraftScope()
        return () => h('div', [h(Child), h(Child)])
      }
    }))
    expect(scope.dirty.value).toBe(false)
    expect(await scope.guard()).toBe(true)
    apis[1]!.draft.name = 'b'
    expect(scope.dirty.value).toBe(true)
    const answer = scope.guard()
    pendingConfirm.value?.resolve(false)
    expect(await answer).toBe(false)
    apis[1]!.reset()
    expect(scope.dirty.value).toBe(false)
    apis[0]!.draft.name = 'c'
    wrapper.unmount()
    expect(scope.dirty.value).toBe(false)
  })
})

describe('Enter in a one-line field', () => {
  it('saves, but not from a checkbox or with a modifier', async () => {
    const { api, save } = setup()
    api.draft.name = 'A'
    const text = document.createElement('input')
    const check = document.createElement('input')
    check.type = 'checkbox'
    const press = (target: HTMLElement, init: KeyboardEventInit = {}) => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true, ...init })
      target.dispatchEvent(event)
      api.onKeydown(Object.defineProperty(event, 'target', { value: target }))
      return event
    }
    expect(press(check).defaultPrevented).toBe(false)
    expect(press(text, { shiftKey: true }).defaultPrevented).toBe(false)
    expect(save).not.toHaveBeenCalled()
    expect(press(text).defaultPrevented).toBe(true)
    await flushPromises()
    expect(save).toHaveBeenCalledTimes(1)
  })
})

describe('keys and in-flight edits', () => {
  it('does not take Enter from a search box or from a form nested in the container', () => {
    const { api } = setup()
    api.draft.name = 'A'
    const container = document.createElement('div')
    const search = document.createElement('input')
    search.type = 'search'
    const nested = document.createElement('form')
    const inner = document.createElement('input')
    nested.append(inner)
    container.append(search, nested)
    const press = (target: HTMLElement) => {
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
      Object.defineProperty(event, 'target', { value: target })
      Object.defineProperty(event, 'currentTarget', { value: container })
      api.onKeydown(event)
      return event.defaultPrevented
    }
    expect(press(search)).toBe(false)
    expect(press(inner)).toBe(false)
  })

  it('ignores Ctrl+S from a dialog opened inside the form', () => {
    const { api, save } = setup()
    api.draft.name = 'A'
    const form = document.createElement('section')
    const dialog = document.createElement('div')
    dialog.setAttribute('role', 'dialog')
    const input = document.createElement('input')
    dialog.append(input)
    form.append(dialog)
    const event = new KeyboardEvent('keydown', { key: 's', ctrlKey: true, cancelable: true })
    Object.defineProperty(event, 'target', { value: input })
    Object.defineProperty(event, 'currentTarget', { value: form })
    api.onKeydown(event)
    expect(event.defaultPrevented).toBe(false)
    expect(save).not.toHaveBeenCalled()
  })

  it('keeps an edit made while the save runs unsaved', async () => {
    let release = () => {}
    const { api } = setup({ save: () => new Promise<void>(resolve => { release = resolve }) })
    api.draft.name = 'A'
    const running = api.save()
    await nextTick()
    api.draft.name = 'B'
    release()
    await running
    expect(api.draft.name).toBe('B')
    expect(api.changed.value).toEqual(['name'])
    api.reset()
    expect(api.draft.name).toBe('A')
  })

  it('lets save override what counts as stored', async () => {
    const { api } = setup({ save: async ({ commit }) => { commit(['lobby'], { lobby: false }) } })
    api.draft.lobby = true
    await api.save()
    expect(api.draft.lobby).toBe(true)
    expect(api.changed.value).toEqual(['lobby'])
  })
})
