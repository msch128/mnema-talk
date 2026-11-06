import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import ThreadReply from './ThreadReply.vue'
import ThreadRootCard from './ThreadRootCard.vue'
import MessageEmbeds from './MessageEmbeds.vue'
import { useMessageActions } from '../composables/useMessageActions'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'
import { messageFixture, userFixture } from '../test-fixtures.fixture'

let w: VueWrapper
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  useAuthStore().user = userFixture({ id: 'me' })
})
afterEach(() => { w?.unmount(); document.body.innerHTML = '' })

const msg = () => messageFixture({ id: 'm1', channel_id: 'ch1', user_id: 'u9', username: 'ada', display_name: 'Ada L.', content: 'hi' })

describe('embeds inside a thread', () => {
  it('know the author and channel for a reply, as in the channel', () => {
    w = mount(ThreadReply, { props: { reply: msg(), actions: useMessageActions() }, attachTo: document.body })
    expect(w.findComponent(MessageEmbeds).props()).toMatchObject({ author: 'Ada L.', authorId: 'u9', channelId: 'ch1' })
  })

  it('know the author and channel for the thread root', () => {
    w = mount(ThreadRootCard, { props: { root: msg(), actions: useMessageActions(), canReact: false }, attachTo: document.body })
    expect(w.findComponent(MessageEmbeds).props()).toMatchObject({ author: 'Ada L.', authorId: 'u9', channelId: 'ch1' })
  })
})
