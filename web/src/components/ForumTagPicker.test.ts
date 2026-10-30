import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ForumTagPicker from './ForumTagPicker.vue'
import { setLocale } from '../i18n'
import { forumTagFixture } from '../test-fixtures.fixture'

const tags = Array.from({ length: 7 }, (_, i) => forumTagFixture({ id: `t${i}`, name: `tag${i}`, position: i + 1, moderated: i === 6 }))

function mountPicker(selected: string[], props: { canModerate?: boolean; required?: boolean } = {}) {
  return mount(ForumTagPicker, {
    props: { tags, labelId: 'lbl', canModerate: false, modelValue: selected, 'onUpdate:modelValue': (v: string[]) => w.setProps({ modelValue: v }), ...props }
  })
}
let w: ReturnType<typeof mountPicker>
const buttons = () => w.findAll('[data-testid="tag-toggle-btn"]')

beforeEach(() => setLocale('en'))

describe('ForumTagPicker', () => {
  it('toggles tags and counts them', async () => {
    w = mountPicker([])
    await buttons()[0]!.trigger('click')
    await buttons()[1]!.trigger('click')
    expect(w.props('modelValue')).toEqual(['t0', 't1'])
    expect(w.text()).toContain('2/5')
    await buttons()[0]!.trigger('click')
    expect(w.props('modelValue')).toEqual(['t1'])
  })

  it('stops at five tags and frees the others when one is dropped', async () => {
    w = mountPicker(['t0', 't1', 't2', 't3', 't4'])
    expect(buttons()[5]!.attributes('disabled')).toBeDefined()
    await buttons()[5]!.trigger('click')
    expect(w.props('modelValue')).toHaveLength(5)
    await buttons()[0]!.trigger('click')
    expect(buttons()[5]!.attributes('disabled')).toBeUndefined()
  })

  it('freezes a moderated tag for members, in either state, but not for moderators', async () => {
    w = mountPicker([])
    expect(buttons()[6]!.attributes('disabled')).toBeDefined()
    await w.setProps({ modelValue: ['t6'] })
    expect(buttons()[6]!.attributes('disabled')).toBeDefined()
    await buttons()[6]!.trigger('click')
    expect(w.props('modelValue')).toEqual(['t6'])

    await w.setProps({ canModerate: true })
    expect(buttons()[6]!.attributes('disabled')).toBeUndefined()
    await buttons()[6]!.trigger('click')
    expect(w.props('modelValue')).toEqual([])
  })

  it('labels the group and says when a tag is required', () => {
    w = mountPicker([], { required: true })
    const group = w.get('[role="group"]')
    expect(w.get(`#${group.attributes('aria-labelledby')}`).text()).toContain('at least one')
  })
})
