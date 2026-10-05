import { describe, it, expect } from 'vitest'
import { renderMarkdown, escapeHtml } from './markdown'

function dom(html) {
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
    expect(root.querySelector('script, iframe, img, svg, object, embed')).toBeNull()
    for (const el of root.querySelectorAll('*')) {
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
    expect(root.querySelector('strong').textContent).toBe('fett')
    expect(root.querySelector('em').textContent).toBe('kursiv')
    expect(root.querySelector('del').textContent).toBe('weg')
    expect(root.querySelector('.discord-spoiler').textContent).toBe('geheim')
  })

  it('does not format inside code', () => {
    const root = dom(renderMarkdown('`**nicht fett**`'))
    expect(root.querySelector('strong')).toBeNull()
    expect(root.querySelector('code').textContent).toBe('**nicht fett**')
  })

  it('links only http(s) URLs and opens them safely', () => {
    const a = dom(renderMarkdown('siehe https://example.com/pfad?x=1.')).querySelector('a')
    expect(a.getAttribute('href')).toBe('https://example.com/pfad?x=1')
    expect(a.getAttribute('rel')).toContain('noopener')
    expect(a.getAttribute('target')).toBe('_blank')
  })

  it('highlights mentions but not e-mail addresses', () => {
    expect(dom(renderMarkdown('hi @Herzog')).querySelector('.md-mention').textContent).toBe('@Herzog')
    expect(dom(renderMarkdown('mail an a@example.com')).querySelector('.md-mention')).toBeNull()
  })

  it('renders block quotes', () => {
    expect(dom(renderMarkdown('> zitat')).querySelector('blockquote').textContent).toBe('zitat')
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
