// The rendered HTML goes into the page through v-html, so these tests look at
// what a browser would build from it: only the tags and attributes the
// renderer owns, never script, handlers, styles or foreign URLs.
import { afterEach, describe, expect, it } from 'vitest'
import { MAX_MARKDOWN_CHARS, registerTextRule, renderMarkdown } from '../markdown'
import { highlightCode, sanitizeHighlighted } from './highlight'

const TAGS = new Set(['STRONG', 'EM', 'U', 'DEL', 'SPAN', 'CODE', 'A', 'H1', 'H2', 'H3', 'DIV', 'BLOCKQUOTE', 'UL', 'OL', 'LI', 'PRE', 'BUTTON'])
const ATTRS: Record<string, readonly string[]> = {
  A: ['href', 'target', 'rel', 'class', 'title', 'data-host', 'data-confirm'],
  SPAN: ['class', 'role', 'aria-expanded', 'aria-label', 'tabindex', 'data-mention', 'data-role-color'],
  BUTTON: ['type', 'class', 'data-md-copy', 'aria-label', 'title'],
  OL: ['class', 'start']
}
const GENERIC = ['class']

function parse(html: string): HTMLElement {
  const template = document.createElement('template')
  template.innerHTML = html
  const root = document.createElement('div')
  root.append(template.content)
  return root
}

function assertSafe(html: string): void {
  for (const el of parse(html).querySelectorAll('*')) {
    expect(TAGS.has(el.tagName), `tag ${el.tagName} in ${html}`).toBe(true)
    for (const attr of el.attributes) {
      expect(ATTRS[el.tagName] ?? GENERIC, `attribute ${el.tagName}.${attr.name} in ${html}`).toContain(attr.name)
      if (attr.name === 'href') expect(attr.value).toMatch(/^(https?:\/\/|mailto:)/i)
    }
  }
}

const PAYLOADS = [
  '<script>alert(1)</script>',
  '<img src=x onerror=alert(1)>',
  '<svg/onload=alert(1)>',
  '"><img src=x onerror=alert(1)>',
  "'><script>alert(1)</script>",
  '</span><img src=x onerror=alert(1)>',
  '</code></pre><script>alert(1)</script>',
  '<a href="javascript:alert(1)">x</a>',
  'javascript:alert(1)',
  'https://example.com/"onmouseover="alert(1)',
  "https://example.com/'onmouseover='alert(1)",
  'https://example.com/<script>',
  '&lt;script&gt;alert(1)&lt;/script&gt;',
  '&#x3C;img src=x onerror=alert(1)&#x3E;',
  '<style>*{display:none}</style>',
  '<iframe srcdoc="<script>alert(1)</script>"></iframe>',
  '\u0000<script>alert(1)</script>',
  '<!--',
  '<![CDATA[',
  '{{constructor.constructor("alert(1)")()}}'
]

const CONTEXTS: ReadonlyArray<(payload: string) => string> = [
  p => p,
  p => `**${p}**`,
  p => `*${p}*`,
  p => `__${p}__`,
  p => `~~${p}~~`,
  p => `||${p}||`,
  p => `\`${p}\``,
  p => `\`\`${p}\`\``,
  p => `> ${p}`,
  p => `>>> ${p}`,
  p => `- ${p}`,
  p => `1. ${p}`,
  p => `# ${p}`,
  p => `-# ${p}`,
  p => `[${p}](https://example.com)`,
  p => `[x](${p})`,
  p => `[x](https://example.com "${p}")`,
  p => `<${p}>`,
  p => `\`\`\`\n${p}\n\`\`\``,
  p => `\`\`\`${p}\n${p}\n\`\`\``,
  p => `\`\`\`js\n${p}\n\`\`\``,
  p => `\\${p}`
]

describe('XSS corpus', () => {
  const cases = PAYLOADS.flatMap(payload => CONTEXTS.map(wrap => wrap(payload)))

  it('renders only the owned tags and attributes, whatever the context', () => {
    for (const input of cases) assertSafe(renderMarkdown(input, { known: new Set(['max']), me: 'max', roles: [{ name: 'Dev', color: '#112233' }] }))
  })

  it('keeps the typed characters visible as text', () => {
    expect(parse(renderMarkdown('<script>alert(1)</script>')).textContent).toBe('<script>alert(1)</script>')
    expect(parse(renderMarkdown('`<img src=x onerror=alert(1)>`')).textContent).toBe('<img src=x onerror=alert(1)>')
  })

  it('never lets a language tag leave the code block', () => {
    for (const lang of ['"><script>', 'js" onclick="x', 'js\u0000', '__proto__', 'constructor', 'a'.repeat(100)]) {
      assertSafe(renderMarkdown(`\`\`\`${lang}\ncode\n\`\`\``))
    }
  })
})

describe('code highlighting output', () => {
  const LANGS = ['js', 'ts', 'html', 'xml', 'markdown', 'php', 'bash', 'sql', 'python', 'css', 'yaml', 'json', 'go', 'rust', 'diff', 'ini', 'java', 'c', 'cpp', 'cs', 'kotlin', 'swift', 'ruby', 'lua', 'powershell', 'dockerfile', 'makefile', 'shell']
  const CODE = ['<img src=x onerror=alert(1)>', '"><script>alert(1)</script>', '</span><img src=x onerror=1>', '<span class="hljs-keyword">x</span>', "'; DROP TABLE x; --", '`${a}` <!-- --> <?php echo 1 ?>']

  it.each(LANGS)('turns %s into text and hljs spans only', lang => {
    for (const code of CODE) {
      const root = parse(renderMarkdown(`\`\`\`${lang}\n${code}\n\`\`\``))
      const body = root.querySelector('pre code')
      expect(body?.textContent).toBe(code)
      for (const el of body?.querySelectorAll('*') ?? []) {
        expect(el.tagName).toBe('SPAN')
        expect([...el.attributes].map(a => a.name)).toEqual(['class'])
        expect(el.getAttribute('class')).toMatch(/^(?:hljs|language)-[A-Za-z0-9_ -]+$/)
      }
    }
  })

  it('drops every tag but plain hljs spans and refuses unbalanced markup', () => {
    expect(sanitizeHighlighted('<span class="hljs-keyword">a</span> &lt; b')).toBe('<span class="hljs-keyword">a</span> &lt; b')
    expect(sanitizeHighlighted('<img src=x onerror=1>')).toBe('&lt;img src=x onerror=1&gt;')
    expect(sanitizeHighlighted('<span class="hljs-a" onclick="x">a</span>')).toBeNull()
    expect(sanitizeHighlighted('<span class="hljs-a">a')).toBeNull()
    expect(sanitizeHighlighted('a</span>')).toBeNull()
    expect(sanitizeHighlighted('a &foo; b & c')).toBe('a &amp;foo; b &amp; c')
    expect(sanitizeHighlighted('a > b')).toBe('a &gt; b')
  })

  it('falls back to plain text when sanitising fails or the block is too big', () => {
    expect(highlightCode('x'.repeat(9000), 'js')).toBeNull()
    expect(highlightCode('x\n'.repeat(400), 'js')).toBeNull()
    expect(highlightCode('let a', 'nope')).toBeNull()
    expect(highlightCode('let a', 'constructor')).toBeNull()
  })
})

describe('extension elements', () => {
  const dispose: Array<() => void> = []
  const register = (build: () => unknown) => dispose.push(registerTextRule({ re: /:evil:/g, build: () => [build()] as never }))
  const run = () => renderMarkdown(':evil:')
  afterEach(() => dispose.splice(0).forEach(fn => fn()))

  it.each([
    [{ t: 'el', tag: 'img', attrs: { src: '/media/emoji/a.png', alt: ':a:', loading: 'lazy', class: 'md-emoji', onerror: 'alert(1)', style: 'x:y' }, c: [] }],
    [{ t: 'el', tag: 'img', attrs: { src: 'javascript:alert(1)', alt: 'x' }, c: [] }],
    [{ t: 'el', tag: 'img', attrs: { src: '//evil.example/a.png', alt: 'x' }, c: [] }],
    [{ t: 'el', tag: 'img', attrs: { src: 'https://evil.example/a.png', alt: 'x' }, c: [] }],
    [{ t: 'el', tag: 'img', attrs: { src: '/a" onerror="alert(1)', alt: 'x' }, c: [] }],
    [{ t: 'el', tag: 'script', attrs: { src: '/x.js' }, c: [{ t: 'text', v: 'alert(1)' }] }],
    [{ t: 'el', tag: 'span', attrs: { class: 'x" onclick="y', 'data-x': 'a\nb', 'data-On click': '1', 'data-ok': '"><b>' }, c: [{ t: 'text', v: '<b>' }] }]
  ])('allows only the attribute allowlist: %j', node => {
    register(() => node)
    const html = run()
    expect(html).not.toMatch(/onerror|onclick|style=|javascript:|evil\.example|<script/i)
    for (const el of parse(html).querySelectorAll('*')) {
      expect(['IMG', 'SPAN']).toContain(el.tagName)
      for (const attr of el.attributes) {
        expect(attr.name).toMatch(/^(src|alt|loading|class|width|height|data-[a-z-]+)$/)
        if (attr.name === 'src') expect(attr.value).toMatch(/^\/(?!\/)/)
      }
    }
  })

  it('renders a valid emoji image with its allowed attributes', () => {
    register(() => ({ t: 'el', tag: 'img', attrs: { src: '/media/emoji/a.png?v=1', alt: ':a:', loading: 'lazy', class: 'md-emoji', width: '22', height: '22' }, c: [] }))
    expect(run()).toBe('<img src="/media/emoji/a.png?v=1" alt=":a:" loading="lazy" class="md-emoji" width="22" height="22">')
  })

  it('degrades an image without a valid source to its alt text', () => {
    register(() => ({ t: 'el', tag: 'img', attrs: { src: 'https://evil.example/a.png', alt: ':a:' }, c: [] }))
    expect(run()).toBe(':a:')
  })

  it('stops applying a rule once it is unregistered', () => {
    register(() => ({ t: 'text', v: 'rewritten' }))
    expect(run()).toBe('rewritten')
    dispose.splice(0).forEach(fn => fn())
    expect(run()).toBe(':evil:')
  })
})

describe('hostile input stays cheap', () => {
  const nasty: ReadonlyArray<readonly [string, string]> = [
    ['stars', '*'.repeat(8000)],
    ['open stars', '*a '.repeat(3000)],
    ['open underscores', ' _a'.repeat(3000)],
    ['open tildes', '~~a '.repeat(2500)],
    ['open spoilers', '||a '.repeat(2500)],
    ['open brackets', '['.repeat(8000)],
    ['open links', '[a]('.repeat(2000)],
    ['backticks', '`'.repeat(8000)],
    ['emphasis around backticks', '*a `b '.repeat(2500)],
    ['emphasis around code spans', '*a `b` '.repeat(2500)],
    ['alternating backticks', '`a``'.repeat(2000)],
    ['fences', '```a\n'.repeat(1500)],
    ['urls', 'http://a'.repeat(1500)],
    ['quotes', '> '.repeat(4000)],
    ['block quotes', '>>> '.repeat(2000)],
    ['list indentation', Array.from({ length: 800 }, (_, i) => `${' '.repeat(i % 40)}- x`).join('\n')],
    ['backslashes', '\\'.repeat(8000)],
    ['nesting', '**__*~~||'.repeat(300)],
    ['nesting closed', `${'**a __b *c ~~d ||'.repeat(40)}${'|| ~~ * __ **'.repeat(40)}`],
    ['headings', '# a\n'.repeat(3000)],
    ['mentions', '@abc '.repeat(3000)],
    ['angle brackets', '<'.repeat(8000)]
  ]

  it.each(nasty)('%s', (_name, input) => {
    const started = performance.now()
    const html = renderMarkdown(input.slice(0, MAX_MARKDOWN_CHARS), { known: new Set(['abc']), roles: [{ name: 'Dev' }] })
    expect(performance.now() - started).toBeLessThan(3000)
    assertSafe(html)
  })

  it('shows text beyond the size limit as it is', () => {
    const big = `**${'a'.repeat(MAX_MARKDOWN_CHARS)}**`
    expect(renderMarkdown(big)).toBe(`**${'a'.repeat(MAX_MARKDOWN_CHARS)}**`)
  })
})
