import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import MessageEmbeds from './MessageEmbeds.vue'
import BotBadge from './BotBadge.vue'
import MessageRow from './MessageRow.vue'
import { messageFixture } from '../test-fixtures.fixture'
import type { ChatEmbed } from '../types/rest'
import { t } from '../i18n'

enableAutoUnmount(afterEach)
beforeEach(() => setActivePinia(createPinia()))

const full: ChatEmbed = {
  title: 'Run 42',
  url: 'https://ci.example/run/42',
  description: 'all **green**',
  color: 0x00ff00,
  author: { name: 'robot', url: 'https://ci.example' },
  fields: [{ name: 'tests', value: '812', inline: true }, { name: 'notes', value: 'long text' }],
  image: { url: 'https://www.cdn.example/chart.png' },
  thumbnail: { url: 'not a url' },
  footer: { text: 'ci' },
  timestamp: '2026-01-02T03:04:05Z'
}

describe('MessageEmbeds', () => {
  it('renders nothing without embeds', () => {
    expect(mount(MessageEmbeds).find('[data-testid="embeds"]').exists()).toBe(false)
  })

  it('renders every part of an embed, with links that cannot reach back', () => {
    const w = mount(MessageEmbeds, { props: { embeds: [full] } })
    const card = w.find('[data-testid="embed"]')
    expect(card.attributes('style')).toContain('border-left-color: #00ff00')
    expect(w.find('[data-testid="embed-author"] a').attributes('href')).toBe('https://ci.example')
    const title = w.find('[data-testid="embed-title"] a')
    expect(title.attributes('href')).toBe('https://ci.example/run/42')
    expect(title.attributes('rel')).toBe('noopener noreferrer')
    expect(title.attributes('target')).toBe('_blank')
    expect(w.text()).toContain('all green')
    const fields = w.findAll('[data-testid="embed-field"]')
    expect(fields).toHaveLength(2)
    expect(fields[0]?.classes()).not.toContain('sm:col-span-3')
    expect(fields[1]?.classes()).toContain('sm:col-span-3')
    expect(w.find('[data-testid="embed-image"]').text()).toContain(`${t('chat.embedImage')} (cdn.example)`)
    expect(w.find('[data-testid="embed-thumbnail"]').text()).toContain('(not a url)')
    expect(w.find('img').exists()).toBe(false)
    expect(w.find('[data-testid="embed-footer"]').text()).toContain('ci')
    expect(w.find('time').attributes('datetime')).toBe('2026-01-02T03:04:05Z')
  })

  it('handles sparse embeds', () => {
    const w = mount(MessageEmbeds, { props: { embeds: [{ title: 'Only a title' }, { author: { name: 'plain' }, footer: { text: 'f' } }, { description: 'd', timestamp: '2026-01-02T03:04:05Z' }] } })
    const cards = w.findAll('[data-testid="embed"]')
    expect(cards).toHaveLength(3)
    expect(cards[0]?.attributes('style')).toBeUndefined()
    expect(cards[0]?.find('[data-testid="embed-title"] a').exists()).toBe(false)
    expect(cards[1]?.find('[data-testid="embed-author"] a').exists()).toBe(false)
    expect(cards[1]?.find('time').exists()).toBe(false)
    expect(cards[2]?.find('[data-testid="embed-footer"]').text()).not.toContain('·')
    const onlyImage = mount(MessageEmbeds, { props: { embeds: [{ image: { url: 'https://a.example/i.png' } }] } })
    expect(onlyImage.find('[data-testid="embed-image"]').exists()).toBe(true)
    expect(onlyImage.find('[data-testid="embed-thumbnail"]').exists()).toBe(false)
    const onlyThumb = mount(MessageEmbeds, { props: { embeds: [{ thumbnail: { url: 'https://a.example/t.png' } }] } })
    expect(onlyThumb.find('[data-testid="embed-image"]').exists()).toBe(false)
    expect(onlyThumb.find('[data-testid="embed-thumbnail"]').exists()).toBe(true)
    const black = mount(MessageEmbeds, { props: { embeds: [{ title: 't', color: 0 }] } })
    expect(black.find('[data-testid="embed"]').attributes('style')).toContain('#000000')
  })
})

describe('webhook messages in the timeline', () => {
  it('marks a bot message and shows its cards', () => {
    const bot = mount(MessageRow, { props: { msg: messageFixture({ display_name: 'CI', is_bot: true, is_webhook: true, embeds: [{ title: 'Run' }] }) } })
    expect(bot.findComponent(BotBadge).text()).toBe(t('chat.webhook'))
    expect(bot.find('[data-testid="embed-title"]').text()).toBe('Run')
    const human = mount(MessageRow, { props: { msg: messageFixture({ display_name: 'Ann' }) } })
    expect(human.findComponent(BotBadge).exists()).toBe(false)
    expect(human.find('[data-testid="embeds"]').exists()).toBe(false)
  })
})
