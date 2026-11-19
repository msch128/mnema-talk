import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import RowSkeleton from './RowSkeleton.vue'

describe('RowSkeleton', () => {
  it('announces one loading status and hides its placeholder rows', () => {
    const w = mount(RowSkeleton, { props: { rows: 4 } })
    expect(w.attributes('role')).toBe('status')
    expect(w.find('[aria-hidden="true"]').findAll('.animate-pulse')).toHaveLength(4)
  })

  it('says what is loading when the list names it', () => {
    const w = mount(RowSkeleton, { props: { label: 'Loading posts…', shape: 'card' } })
    expect(w.text()).toBe('Loading posts…')
  })

  it('holds still with reduced motion', () => {
    const row = mount(RowSkeleton).find('.animate-pulse')
    expect(row.classes()).toContain('motion-reduce:animate-none')
  })
})
