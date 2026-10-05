import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import EditNameDialog from './EditNameDialog.vue'
import { i18nPlugin } from '../i18n'
import { tooltip } from '../directives/tooltip'

const mountOpts = { global: { plugins: [i18nPlugin], directives: { tooltip } } }

let wrapper
beforeEach(() => setActivePinia(createPinia()))
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('EditNameDialog', () => {
  it('shows a translated message when the name is blank', async () => {
    wrapper = mount(EditNameDialog, { ...mountOpts, props: { kind: 'category', entity: { id: 'c1', name: '   ' } } })
    await wrapper.find('form').trigger('submit')
    const alert = wrapper.find('[role="alert"]')
    expect(alert.exists()).toBe(true)
    expect(alert.text()).toBe('Bitte gib einen Namen an.')
  })
})
