import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import OutgoingWebhookForm from './OutgoingWebhookForm.vue'
import { fixtureId } from '../../test-fixtures.fixture'
import { setLocale } from '../../i18n'

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
})

describe('new outgoing webhook form', () => {
  it('says why Save is unavailable when only the URL is filled in', async () => {
    const w = mount(OutgoingWebhookForm, { props: { channelId: fixtureId(2) } })
    await w.find('input[type="url"]').setValue('https://ci.example/hook')
    const name = w.find('input[type="text"]')
    const error = w.find('[data-testid="outgoing-name-error"]')
    expect(error.text()).not.toBe('')
    expect(name.attributes('aria-invalid')).toBe('true')
    expect(name.attributes('aria-describedby')).toBe(error.attributes('id'))
    expect(w.find('[data-testid="outgoing-url-error"]').exists()).toBe(false)
  })

  it('says why Save is unavailable when only the name is filled in', async () => {
    const w = mount(OutgoingWebhookForm, { props: { channelId: fixtureId(2) } })
    await w.find('input[type="text"]').setValue('CI')
    const url = w.find('input[type="url"]')
    const error = w.find('[data-testid="outgoing-url-error"]')
    expect(error.text()).not.toBe('')
    expect(url.attributes('aria-describedby')).toBe(error.attributes('id'))
  })
})
