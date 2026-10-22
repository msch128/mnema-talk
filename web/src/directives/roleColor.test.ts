import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, withDirectives, nextTick } from 'vue'
import { roleColors, isRoleHexColor } from './roleColor'

function host(html: string) {
  const component = defineComponent((props: { html: string }) =>
    () => withDirectives(h('div', { innerHTML: props.html }), [[roleColors]]),
  { props: { html: { type: String, required: true } } })
  return mount(component, { props: { html } })
}
const pill = (w: ReturnType<typeof host>) => (w.element as HTMLElement).querySelector<HTMLElement>('.pill')

describe('v-role-colors', () => {
  it('sets --role-color through the CSSOM for a valid colour', () => {
    const w = host('<span class="pill" data-role-color="#3366cc">@Dev</span>')
    expect(pill(w)?.style.getPropertyValue('--role-color')).toBe('#3366cc')
  })

  it('ignores anything but #rrggbb', () => {
    for (const bad of ['red', '#fff', 'url(x)', '#3366cc; background:red', '']) {
      const w = host(`<span class="pill" data-role-color="${bad}">@Dev</span>`)
      expect(pill(w)?.style.getPropertyValue('--role-color')).toBe('')
    }
  })

  it('repaints when the markup changes', async () => {
    const w = host('<span class="pill" data-role-color="#3366cc">@Dev</span>')
    await w.setProps({ html: '<span class="pill" data-role-color="#aa0000">@Dev</span>' })
    await nextTick()
    expect(pill(w)?.style.getPropertyValue('--role-color')).toBe('#aa0000')
  })

  it('validates colours', () => {
    expect(isRoleHexColor('#AbCdEf')).toBe(true)
    expect(isRoleHexColor('AbCdEf')).toBe(false)
    expect(isRoleHexColor(null)).toBe(false)
  })
})
