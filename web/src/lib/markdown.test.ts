import { describe, it, expect } from 'vitest'
import { renderMarkdown, escapeHtml } from './markdown'

function dom(html: string) {
  const div = document.createElement('div')
  div.innerHTML = html
  return div
}

describe('renderMarkdown – security', () => {
  const payloads = [
    '<script>alert(1)</script>',
    '<img src=x onerror=alert(1)>',
    '<a href="javascript:alert(1)">x</a>',
    '"><svg onload=alert(1)>',
    '**<img src=x onerror=alert(1)>**',
    '||<iframe src=//evil>||',
    '`<script>`',
    '```\n<script>alert(1)</script>\n```',
    'https://evil.example/"onmouseover="alert(1)',
    "https://evil.example/'onmouseover='alert(1)",
    '[x](javascript:alert(1))',
    'javascript:alert(1)'
  ]

  it.each(payloads)('never produces script, handlers or javascript: URLs for %s', input => {
    const root = dom(renderMarkdown(input))
    expect(root.querySelector<HTMLElement>('script, iframe, img, svg, object, embed')).toBeNull()
    for (const el of root.querySelectorAll<HTMLElement>('*')) {
      for (const attr of el.attributes) {
        expect(attr.name.startsWith('on')).toBe(false)
        expect(attr.value.toLowerCase()).not.toContain('javascript:')
      }
    }
  })

  it('emits no inline event handlers for spoilers (CSP forbids them)', () => {
    expect(renderMarkdown('||secret||')).not.toMatch(/onclick/i)
  })

  it('escapes all HTML special characters', () => {
    expect(escapeHtml(`<a href="x" title='y'>&</a>`)).toBe('&lt;a href=&quot;x&quot; title=&#39;y&#39;&gt;&amp;&lt;/a&gt;')
  })
})

describe('renderMarkdown – formatting', () => {
  it('renders bold, italic, strike and spoilers', () => {
    const root = dom(renderMarkdown('**fett** *kursiv* ~~weg~~ ||geheim||'))
    expect(root.querySelector<HTMLElement>('strong')?.textContent).toBe('fett')
    expect(root.querySelector<HTMLElement>('em')?.textContent).toBe('kursiv')
    expect(root.querySelector<HTMLElement>('del')?.textContent).toBe('weg')
    expect(root.querySelector<HTMLElement>('.md-spoiler')?.textContent).toBe('geheim')
  })

  it('does not format inside code', () => {
    const root = dom(renderMarkdown('`**not bold**`'))
    expect(root.querySelector<HTMLElement>('strong')).toBeNull()
    expect(root.querySelector<HTMLElement>('code')?.textContent).toBe('**not bold**')
  })

  it('links only http(s) URLs and opens them safely', () => {
    const a = dom(renderMarkdown('siehe https://example.com/pfad?x=1.')).querySelector<HTMLElement>('a')
    expect(a?.getAttribute('href')).toBe('https://example.com/pfad?x=1')
    expect(a?.getAttribute('rel')).toContain('noopener')
    expect(a?.getAttribute('target')).toBe('_blank')
  })

  it('highlights mentions but not e-mail addresses', () => {
    expect(dom(renderMarkdown('hi @Herzog')).querySelector<HTMLElement>('.md-mention')?.textContent).toBe('@Herzog')
    expect(dom(renderMarkdown('mail an a@example.com')).querySelector<HTMLElement>('.md-mention')).toBeNull()
  })

  it('renders block quotes', () => {
    expect(dom(renderMarkdown('> zitat')).querySelector<HTMLElement>('blockquote')?.textContent).toBe('zitat')
  })
})

describe('links with mention-like or formatting characters', () => {
  it('keeps a URL containing (@name intact', () => {
    const html = renderMarkdown('see https://x.com/(@abcd and @max')
    expect(html).toContain('<a href="https://x.com/(@abcd"')
    expect(html).toContain('>https://x.com/(@abcd</a>')
    expect(html).toContain('<span class="md-mention">@max</span>')
    expect(html.match(/md-mention/g)).toHaveLength(1)
  })

  it('does not format characters inside URLs but formats around them', () => {
    const html = renderMarkdown('**https://x.com/a*b*c**')
    expect(html).toContain('<strong><a href="https://x.com/a*b*c"')
  })
})

describe('mentions with known members', () => {
  const known = new Set(['max', 'herzog'])
  it('highlights only members and @all/@here, marks the reader', () => {
    const html = renderMarkdown('@max, @nobody, @all and @herzog.', { known, me: 'Herzog' })
    const d = dom(html)
    const names = [...d.querySelectorAll<HTMLElement>('.md-mention')].map(e => e.dataset.mention)
    expect(names).toEqual(['max', 'all', 'herzog'])
    expect(d.querySelector<HTMLElement>('[data-mention="all"]')?.classList.contains('md-mention-group')).toBe(true)
    expect(d.querySelector<HTMLElement>('[data-mention="herzog"]')?.classList.contains('md-mention-me')).toBe(true)
    expect(d.textContent).toBe('@max, @nobody, @all and @herzog.')
  })
})

describe('role mentions', () => {
  const roles = [{ name: 'Dev' }, { name: 'Dev Team', me: true }, { name: 'R&D' }]
  const pills = (text: string) => [...dom(renderMarkdown(text, { roles })).querySelectorAll<HTMLElement>('.md-mention-role')]

  it('draws the recorded roles as pills, longest name first, case-insensitively', () => {
    expect(pills('hi @dev, and @DEV team!').map(e => e.textContent)).toEqual(['@dev', '@DEV team'])
    expect(pills('@Dev Team standup')[0]?.classList.contains('md-mention-me')).toBe(true)
    expect(pills('@dev')[0]?.classList.contains('md-mention-me')).toBe(false)
  })

  it('needs a word boundary and escapes the name', () => {
    expect(pills('mail@dev.example and @developer and @dev-ops')).toHaveLength(0)
    expect(pills('ping @r&d now').map(e => e.textContent)).toEqual(['@r&d'])
    expect(renderMarkdown('@dev', { roles: [] })).not.toContain('md-mention-role')
  })

  it('carries only a valid #rrggbb colour, as data and never as a style attribute', () => {
    const coloured = (color: string) => dom(renderMarkdown('@dev', { roles: [{ name: 'Dev', color }] })).querySelector('.md-mention-role')
    expect(coloured('#3366cc')?.getAttribute('data-role-color')).toBe('#3366cc')
    expect(coloured('#3366cc')?.hasAttribute('style')).toBe(false)
    for (const bad of ['red', '#fff', 'url(x)', '#3366cc"onmouseover="x', '']) {
      expect(coloured(bad)?.hasAttribute('data-role-color')).toBe(false)
    }
  })

  it('leaves role pills non-interactive and keeps user mentions working', () => {
    const d = dom(renderMarkdown('@dev and @max', { roles, known: new Set(['max']) }))
    expect(d.querySelector('.md-mention-role')?.hasAttribute('data-mention')).toBe(false)
    expect(d.querySelector('[data-mention="max"]')).not.toBeNull()
  })
})
