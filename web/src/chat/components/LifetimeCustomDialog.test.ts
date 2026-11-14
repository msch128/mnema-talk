import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LifetimeCustomDialog from './LifetimeCustomDialog.vue'
import { setLocale } from '../../i18n'

// A lifetime of the member's own: it opens on the current choice in its
// largest whole unit, takes only what the community allows and hands back
// seconds (doc/timed-messages.md).

enableAutoUnmount(afterEach)

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
})

function open(initial: number, max = 2592000) {
  const w = mount(LifetimeCustomDialog, { props: { initial, max }, attachTo: document.body })
  const amount = () => document.body.querySelector<HTMLInputElement>('#lifetime-amount')!
  const unit = () => document.body.querySelector<HTMLSelectElement>('#lifetime-unit')!
  const submit = () => document.body.querySelector<HTMLButtonElement>('[data-testid="lifetime-custom"] button[type="submit"]')!
  return { w, amount, unit, submit }
}

async function set(el: HTMLInputElement | HTMLSelectElement, value: string) {
  el.value = value
  el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input'))
  await flushPromises()
}

describe('the custom lifetime', () => {
  it.each([
    [2 * 86400, '2', 'days'],
    [3 * 3600, '3', 'hours'],
    [90 * 60, '90', 'minutes'],
    [0, '10', 'minutes']
  ])('opens a choice of %i s as %s %s', async (initial, amount, unit) => {
    const d = open(initial)
    await flushPromises()
    expect(d.amount().value).toBe(amount)
    expect(d.unit().value).toBe(unit)
  })

  it('hands back the seconds of the number and unit chosen', async () => {
    const d = open(0)
    await flushPromises()
    await set(d.amount(), '5')
    await set(d.unit(), 'hours')
    d.submit().click()
    await flushPromises()
    expect(d.w.emitted('pick')).toEqual([[5 * 3600]])
  })

  it('refuses a lifetime beyond the community\'s ceiling, and takes nothing on Enter', async () => {
    const d = open(3600, 3600)
    await flushPromises()
    await set(d.amount(), '2')
    expect(d.amount().getAttribute('aria-invalid')).toBe('true')
    expect(d.submit().disabled).toBe(true)
    document.body.querySelector<HTMLFormElement>('[data-testid="lifetime-custom"]')!.requestSubmit()
    await flushPromises()
    expect(d.w.emitted('pick')).toBeUndefined()
  })

  it('closes on Cancel without a choice', async () => {
    const d = open(3600)
    await flushPromises()
    ;[...document.body.querySelectorAll<HTMLButtonElement>('[data-testid="lifetime-custom"] button')].find(b => b.textContent?.trim() === 'Cancel')!.click()
    await flushPromises()
    expect(d.w.emitted('close')).toHaveLength(1)
    expect(d.w.emitted('pick')).toBeUndefined()
  })
})
