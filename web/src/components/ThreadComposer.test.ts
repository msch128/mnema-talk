import { beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ThreadComposer from './ThreadComposer.vue'
import { setLocale } from '../i18n'

beforeEach(() => { setActivePinia(createPinia()); setLocale('en') })

const mountComposer = (modelValue: string) => mount(ThreadComposer, {
  props: { modelValue, canSend: true },
  global: { directives: { tooltip: () => undefined } }
})

describe('ThreadComposer', () => {
  it('tells that commands and polls start in the channel when the text begins like one', () => {
    const w = mountComposer('/poll Lunch? | Pizza | Pasta')
    expect(w.get('[data-testid="thread-command-hint"]').text()).toBe('Commands and polls start in the channel. Here this text is sent as a reply.')
  })

  it('stays quiet for ordinary text and for a path in the middle of a sentence', () => {
    expect(mountComposer('see /etc/hosts').find('[data-testid="thread-command-hint"]').exists()).toBe(false)
    expect(mountComposer('hello').find('[data-testid="thread-command-hint"]').exists()).toBe(false)
  })
})
