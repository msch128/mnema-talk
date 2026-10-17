import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PermissionToggles from './PermissionToggles.vue'
import { PERMISSION_BITS } from '../lib/permissions'
import { setLocale } from '../i18n'

const keys = ['send_messages', 'speak'] as const

describe('PermissionToggles', () => {
  it('shows names and explanations and flips switches', async () => {
    setLocale('en')
    const w = mount(PermissionToggles, { props: { keys, allow: PERMISSION_BITS.speak } })
    expect(w.text()).toContain('Send messages')
    expect(w.text()).toContain('Write, reply and edit own messages.')
    const [send, speak] = w.findAll('[role="switch"]')
    expect(send!.attributes('aria-checked')).toBe('false')
    expect(speak!.attributes('aria-checked')).toBe('true')
    await send!.trigger('click')
    await speak!.trigger('click')
    expect(w.emitted('update:allow')).toEqual([[PERMISSION_BITS.speak | PERMISSION_BITS.send_messages], [0]])
  })

  it('disables locked and read-only switches', () => {
    const locked = mount(PermissionToggles, { props: { keys, locked: PERMISSION_BITS.speak } })
    expect(locked.findAll('[role="switch"]').map(b => b.attributes('disabled') !== undefined)).toEqual([false, true])
    const readonly = mount(PermissionToggles, { props: { keys, readonly: true } })
    expect(readonly.findAll('[role="switch"]').every(b => b.attributes('disabled') !== undefined)).toBe(true)
  })

  it('sets deny, inherit and allow per permission in overwrite mode', async () => {
    setLocale('en')
    const w = mount(PermissionToggles, { props: { keys, mode: 'tri', allow: PERMISSION_BITS.speak, deny: PERMISSION_BITS.send_messages } })
    const group = (i: number) => w.findAll('[role="group"]')[i]!
    expect(group(0).find('[aria-pressed="true"]').text()).toBe('Deny')
    expect(group(1).find('[aria-pressed="true"]').text()).toBe('Allow')
    await group(0).find('[data-state="allow"]').trigger('click')
    await group(1).find('[data-state="inherit"]').trigger('click')
    await group(1).find('[data-state="deny"]').trigger('click')
    expect(w.emitted('update:allow')).toEqual([[PERMISSION_BITS.speak | PERMISSION_BITS.send_messages], [0], [0]])
    expect(w.emitted('update:deny')).toEqual([[0], [PERMISSION_BITS.send_messages], [PERMISSION_BITS.send_messages | PERMISSION_BITS.speak]])
    const inherit = mount(PermissionToggles, { props: { keys, mode: 'tri' } })
    expect(inherit.findAll('[aria-pressed="true"]').map(b => b.text())).toEqual(['Inherit', 'Inherit'])
  })
})
