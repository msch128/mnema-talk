import { afterEach, describe, expect, it } from 'vitest'
import { markdownToPlain, previewLinks, registerTextRule, renderMarkdown } from '../markdown'
import type { Inline } from './types'

const dispose: Array<() => void> = []
afterEach(() => dispose.splice(0).forEach(fn => fn()))

const pill = (name: string): Inline[] => [{ t: 'el', tag: 'span', attrs: { class: 'pill', 'data-name': name }, c: [{ t: 'text', v: `:${name}:` }] }]

function registerPill(inLinks = false) {
  dispose.push(registerTextRule({
    re: /:([a-z]{2,8}):/,
    build: m => pill(m[1] ?? ''),
    plain: m => `[${m[1]}]`,
    inLinks
  }))
}

describe('registered text rules', () => {
  it('turn their token into nodes in text and inside formatting, headings, lists and quotes', () => {
    registerPill()
    const html = renderMarkdown('a :one: **b :two:** # :three:\n- :four:\n> :five:')
    expect([...html.matchAll(/data-name="(\w+)"/g)].map(m => m[1])).toEqual(['one', 'two', 'three', 'four', 'five'])
  })

  it('leave code, code blocks and link labels alone unless they ask for links', () => {
    registerPill()
    const html = renderMarkdown('`:one:` [:two:](https://example.com) ```\n:three:\n```')
    expect(html).not.toContain('data-name')
    registerPill(true)
    expect(renderMarkdown('[:two:](https://example.com)')).toContain('data-name="two"')
  })

  it('apply to every plain-text preview through their own plain form', () => {
    registerPill()
    expect(markdownToPlain('hi :one: **:two:**')).toBe('hi [one] [two]')
    expect(markdownToPlain('`:one:`')).toBe(':one:')
  })

  it('can decline a match and leave the text as typed', () => {
    dispose.push(registerTextRule({ re: /:x:/, build: () => null }))
    expect(renderMarkdown('a :x: b')).toBe('a :x: b')
  })

  it('run before the per-render mentions, which keep working next to them', () => {
    registerPill()
    const html = renderMarkdown(':one: @max', { known: new Set(['max']) })
    expect(html).toContain('data-name="one"')
    expect(html).toContain('data-mention="max"')
  })

  it('ignore zero-length matches', () => {
    dispose.push(registerTextRule({ re: /x*/, build: () => pill('zero') }))
    expect(renderMarkdown('abc')).toBe('abc')
  })
})

describe('preview links', () => {
  it('lists links shown as links, but not code, spoilers, <autolinks> or other schemes', () => {
    const md = 'https://a.test [b](https://b.test) <https://c.test> `https://d.test` ||https://e.test|| [m](mailto:x@y.test)\n- https://f.test\n> https://g.test\n```\nhttps://h.test\n```'
    expect(previewLinks(md)).toEqual(['https://a.test', 'https://b.test', 'https://f.test', 'https://g.test'])
    expect(previewLinks(null)).toEqual([])
  })
})

describe('mention rules', () => {
  const known = new Set(['max', 'cool_guy'])

  it('keep the trailing dot or dash outside the pill', () => {
    expect(renderMarkdown('hi @max.', { known })).toBe('hi <span class="md-mention" data-mention="max" role="button" tabindex="0">@max</span>.')
  })

  it('work inside formatting, quotes, lists and headings', () => {
    const html = renderMarkdown('**@max** > @max\n> @max\n- @max\n# @max', { known })
    expect(html.match(/data-mention="max"/g)).toHaveLength(5)
  })

  it('do not fire inside code, links or e-mail addresses', () => {
    const html = renderMarkdown('`@max` [@max](https://example.com) a@max.example', { known })
    expect(html).not.toContain('data-mention')
  })

  it('highlight any name when no member list is known, without click data', () => {
    expect(renderMarkdown('@Anyone')).toBe('<span class="md-mention">@Anyone</span>')
  })
})
