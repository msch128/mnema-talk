import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import EventCoverField from './EventCoverField.vue'
import { setLocale } from '../../i18n'

describe('EventCoverField', () => {
  it('opens the file chooser from a real button and keeps the raw input out of the tab order', async () => {
    setLocale('en')
    const w = mount(EventCoverField, { props: { preview: null } })
    const input = w.get<HTMLInputElement>('input[type="file"]')
    expect(input.attributes('tabindex')).toBe('-1')
    expect(input.classes()).not.toContain('hidden')
    const click = vi.spyOn(input.element, 'click')
    await w.get('[data-testid="event-cover-choose"]').trigger('click')
    expect(click).toHaveBeenCalled()
  })
})
