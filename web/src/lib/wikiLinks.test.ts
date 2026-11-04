import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './markdown'
import type { WikiPageSummary } from './wikiApi'
import { wikiLinkRule } from './wikiLinks'

const pages: WikiPageSummary[] = [
  { id: '1', slug: 'raid-guide', title: 'Raid guide', parent_id: null, position: 0, locked: false, updated_at: '2026-01-01T00:00:00Z' }
]

function render(text: string) {
  const root = document.createElement('div')
  root.innerHTML = renderMarkdown(text, { document: true, rules: [wikiLinkRule(pages)] })
  return root
}

describe('[[wiki links]]', () => {
  it('links a page by title, with an optional label', () => {
    const link = render('See [[raid guide]] and [[Raid guide|the guide]].').querySelectorAll<HTMLElement>('.md-wikilink')
    expect(link).toHaveLength(2)
    expect(link[0]?.dataset['wiki']).toBe('raid-guide')
    expect(link[0]?.textContent).toBe('raid guide')
    expect(link[1]?.textContent).toBe('the guide')
    expect(link[0]?.getAttribute('role')).toBe('button')
  })

  it('shows a missing page as plain text with the missing class', () => {
    const el = render('[[Nowhere]]').querySelector<HTMLElement>('.md-wikilink')
    expect(el?.classList.contains('md-wikilink-missing')).toBe(true)
    expect(el?.dataset['wiki']).toBeUndefined()
  })

  it('does not match unclosed or nested brackets, or a line break', () => {
    expect(render('[[open').querySelector('.md-wikilink')).toBeNull()
    expect(render('[[a\nb]]').querySelector('.md-wikilink')).toBeNull()
    expect(render('[[a[b]]').querySelector('.md-wikilink')).toBeNull()
  })

  it('stays fast on pathological input', () => {
    const start = performance.now()
    render('[['.repeat(5000) + 'x' + ']]'.repeat(5000))
    expect(performance.now() - start).toBeLessThan(2000)
  })
})

describe('the wiki renderer never lets a page run script', () => {
  const corpus = [
    '<script>alert(1)</script>',
    '<img src=x onerror=alert(1)>',
    '[[<img src=x onerror=alert(1)>]]',
    '[[x|<script>alert(1)</script>]]',
    '[click](javascript:alert(1))',
    '[click](JaVaScRiPt:alert(1))',
    '[click]( javascript:alert(1))',
    '[click](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)',
    '![x](javascript:alert(1))',
    '![x](https://evil.example/x.png" onerror="alert(1))',
    '<a href="javascript:alert(1)">x</a>',
    '<iframe src="//evil.example"></iframe>',
    '<svg onload=alert(1)>',
    '<style>body{display:none}</style>',
    '<div onclick="alert(1)">x</div>',
    '<form action="//evil.example"><input name=x></form>',
    '<meta http-equiv="refresh" content="0;url=//evil.example">',
    '<base href="//evil.example/">',
    '<math><mtext><table><mglyph><style><img src=x onerror=alert(1)>',
    '"><script>alert(1)</script>',
    '**<img src=x onerror=alert(1)>**',
    '```\n<script>alert(1)</script>\n```',
    '[[raid guide]]<script>alert(1)</script>',
    '[x]: javascript:alert(1)\n[x]',
    'https://evil.example/"onmouseover="alert(1)'
  ]

  it.each(corpus)('%#: no executable element, handler or script URL', input => {
    const root = render(input)
    expect(root.querySelector('script, iframe, object, embed, style, form, input, meta, base, link, svg, math')).toBeNull()
    for (const el of root.querySelectorAll<HTMLElement>('*')) {
      for (const attr of el.attributes) {
        expect(attr.name.startsWith('on')).toBe(false)
        if (attr.name === 'href' || attr.name === 'src') expect(attr.value).not.toMatch(/^\s*(javascript|data|vbscript):/i)
      }
    }
  })

  it('only the wiki data attribute and the button role are added by a link', () => {
    const el = render('[[Raid guide]]').querySelector<HTMLElement>('.md-wikilink')!
    expect([...el.attributes].map(a => a.name).sort()).toEqual(['class', 'data-wiki', 'role', 'tabindex'])
  })
})

describe('wiki links in plain text', () => {
  it('shows the label, or the target, without brackets', () => {
    const rule = wikiLinkRule(pages)
    const plain = (text: string) => rule.plain?.(new RegExp(rule.re.source).exec(text) as RegExpExecArray)
    expect(plain('[[Raid guide|the guide]]')).toBe('the guide')
    expect(plain('[[Raid guide]]')).toBe('Raid guide')
  })

  it('falls back to the target when the label is blank', () => {
    expect(render('[[Raid guide|   ]]').querySelector('.md-wikilink')?.textContent).toBe('Raid guide')
  })
})
