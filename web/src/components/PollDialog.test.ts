import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import PollDialog from './PollDialog.vue'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { setLocale, i18nPlugin } from '../i18n'
import { channelFixture, fixtureId, messageFixture } from '../test-fixtures.fixture'

const CH = fixtureId(2)
let requests: { url: string; method: string; body: unknown }[]
let status: number

function mountDialog(features: string[] = ['polls.anonymous']) {
  useServerFeaturesStore().list = features
  const chat = useChatStore()
  chat.activeChannel = channelFixture({ id: CH })
  return mount(PollDialog, { props: { channelId: CH }, attachTo: document.body, global: { plugins: [i18nPlugin] } })
}

const answers = (w: ReturnType<typeof mountDialog>) => w.findAll('[data-testid="poll-answer"]')
const post = () => document.querySelector<HTMLButtonElement>('[data-testid="poll-post"]')

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  requests = []
  status = 201
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    requests.push({ url: String(url), method: init?.method || 'GET', body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined })
    const body = status === 201
      ? messageFixture({ id: fixtureId(9), channel_id: CH, content: 'Lunch?' })
      : { error: { code: 'INVALID_INPUT', message: 'options must differ' } }
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('PollDialog', () => {
  it('cannot be posted until it has a question and two answers, and says what is missing', async () => {
    const w = mountDialog()
    expect(post()?.disabled).toBe(true)
    expect(w.get('[data-testid="poll-problem"]').text()).toBe('Enter a question.')
    await w.get('#poll-question').setValue('Lunch?')
    expect(w.get('[data-testid="poll-problem"]').text()).toBe('Add at least 2 answers.')
    await answers(w)[0]!.setValue('Pizza')
    await answers(w)[1]!.setValue('pizza')
    expect(w.get('[data-testid="poll-problem"]').text()).toBe('Answers must differ.')
    await answers(w)[1]!.setValue('Pasta')
    expect(w.find('[data-testid="poll-problem"]').exists()).toBe(false)
    expect(post()?.disabled).toBe(false)
  })

  it('posts the poll with its settings and closes', async () => {
    const w = mountDialog()
    await w.get('#poll-question').setValue('  Lunch?  ')
    await answers(w)[0]!.setValue('Pizza')
    await answers(w)[1]!.setValue('Pasta')
    await w.get('[data-testid="poll-add"]').trigger('click')
    await answers(w)[2]!.setValue('Salad')
    await w.get('#poll-duration').setValue('3d')
    await w.get('[data-testid="poll-multiple"]').setValue(true)
    await w.get('[data-testid="poll-anonymous-toggle"]').setValue(true)
    expect(w.get('[data-testid="poll-anonymous-hint"]').text()).toContain('A vote can’t be changed once it is sent.')

    await w.get('[data-testid="poll-dialog"]').trigger('submit')
    await flushPromises()
    expect(requests).toEqual([{
      url: `/api/channels/${CH}/polls`, method: 'POST',
      body: { question: 'Lunch?', options: ['Pizza', 'Pasta', 'Salad'], duration: '3d', multiple: true, anonymous: true }
    }])
    expect(w.emitted('posted')).toHaveLength(1)
    expect(w.emitted('close')).toHaveLength(1)
  })

  it('offers no anonymous toggle without the server feature and never asks for one', async () => {
    const w = mountDialog([])
    expect(w.find('[data-testid="poll-anonymous-toggle"]').exists()).toBe(false)
    await w.get('#poll-question').setValue('Lunch?')
    await answers(w)[0]!.setValue('Pizza')
    await answers(w)[1]!.setValue('Pasta')
    await w.get('[data-testid="poll-dialog"]').trigger('submit')
    await flushPromises()
    expect(requests[0]?.body).toMatchObject({ anonymous: false })
  })

  it('does not post twice or an invalid poll', async () => {
    const w = mountDialog()
    await w.get('[data-testid="poll-dialog"]').trigger('submit')
    expect(requests).toEqual([])
  })

  it('keeps between 2 and 10 answer fields', async () => {
    const w = mountDialog()
    expect(answers(w)).toHaveLength(2)
    expect(w.get('[aria-label="Remove answer 1"]').attributes('disabled')).toBeDefined()
    for (let i = 0; i < 12; i++) await w.get('[data-testid="poll-add"]').trigger('click')
    expect(answers(w)).toHaveLength(10)
    expect(w.get('[data-testid="poll-add"]').attributes('disabled')).toBeDefined()
    await w.get('[aria-label="Remove answer 10"]').trigger('click')
    expect(answers(w)).toHaveLength(9)
  })

  it('shows the server\'s refusal and stays open', async () => {
    status = 400
    const w = mountDialog()
    await w.get('#poll-question').setValue('Lunch?')
    await answers(w)[0]!.setValue('A')
    await answers(w)[1]!.setValue('B')
    await w.get('[data-testid="poll-dialog"]').trigger('submit')
    await flushPromises()
    expect(w.get('[role="alert"]').text()).toContain('The options must all be different.')
    expect(w.emitted('close')).toBeUndefined()
  })

  it('closes without posting', async () => {
    const w = mountDialog()
    await w.findAll('button').find(b => b.text() === 'Cancel')!.trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    expect(requests).toEqual([])
  })
})
