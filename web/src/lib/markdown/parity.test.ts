// Discord's Markdown semantics, one row per behaviour: [input, rendered HTML].
// The rendered text of a message sits in a white-space: pre-wrap box, so block
// elements swallow the line break next to them and plain lines keep theirs.
import { describe, expect, it } from 'vitest'
import { markdownToPlain, renderMarkdown } from '../markdown'

const OUT = 'target="_blank" rel="noopener noreferrer nofollow"'
const link = (href: string, text = href) => `<a href="${href}" ${OUT} class="md-link">${text}</a>`
const masked = (href: string, host: string, label: string, confirm = true) =>
  `<a href="${href}" ${OUT} class="md-link md-link-masked" title="${href}" data-host="${host}"${confirm ? ' data-confirm="1"' : ''}>${label}</a>`
const spoiler = (inner: string) => `<span class="md-spoiler" role="button" tabindex="0">${inner}</span>`
const underline = (inner: string) => `<u class="md-underline">${inner}</u>`

type Row = readonly [input: string, html: string]

function table(rows: readonly Row[]) {
  it.each(rows)('%j', (input, html) => {
    expect(renderMarkdown(input)).toBe(html)
  })
}

describe('emphasis', () => {
  table([
    ['**bold**', '<strong>bold</strong>'],
    ['*italic*', '<em>italic</em>'],
    ['_italic_', '<em>italic</em>'],
    ['__underline__', underline('underline')],
    ['~~strike~~', '<del>strike</del>'],
    ['||secret||', spoiler('secret')],
    ['a **b** c', 'a <strong>b</strong> c'],
    ['**two words**', '<strong>two words</strong>'],
    ['**bold\nacross lines**', '<strong>bold\nacross lines</strong>'],
    ['a*b*c', 'a<em>b</em>c'],
    ['snake_case_name', 'snake_case_name'],
    ['_a_b', '_a_b'],
    ['2 * 3 * 4', '2 * 3 * 4'],
    ['* not italic *', '<ul class="md-list"><li>not italic *</li></ul>'],
    ['~~ not struck~~', '~~ not struck~~'],
    ['unclosed **bold', 'unclosed **bold'],
    ['unclosed *it', 'unclosed *it'],
    ['unclosed ||spoiler', 'unclosed ||spoiler'],
    ['****', '****'],
    ['** **', '<strong> </strong>']
  ])
})

describe('combinations', () => {
  table([
    ['***bold italic***', '<strong><em>bold italic</em></strong>'],
    ['__*underline italic*__', underline('<em>underline italic</em>')],
    ['__**underline bold**__', underline('<strong>underline bold</strong>')],
    ['**__*all three*__**', `<strong>${underline('<em>all three</em>')}</strong>`],
    ['~~**strike bold**~~', '<del><strong>strike bold</strong></del>'],
    ['||**hidden bold**||', spoiler('<strong>hidden bold</strong>')],
    ['*a **b** c*', '<em>a <strong>b</strong> c</em>'],
    ['**a *b* c**', '<strong>a <em>b</em> c</strong>'],
    ['||`code` in spoiler||', spoiler('<code class="md-code">code</code> in spoiler')],
    ['**`code`**', '<strong><code class="md-code">code</code></strong>']
  ])
})

describe('escapes', () => {
  table([
    ['\\*not italic\\*', '*not italic*'],
    ['\\**kept**', '**kept**'],
    ['\\_x\\_', '_x_'],
    ['\\~\\~x\\~\\~', '~~x~~'],
    ['\\|\\|x\\|\\|', '||x||'],
    ['\\`not code\\`', '`not code`'],
    ['\\\\', '\\'],
    ['\\# not a heading', '# not a heading'],
    ['back\\slash', 'back\\slash'],
    ['\\<b>', '&lt;b&gt;']
  ])
})

describe('inline code', () => {
  table([
    ['`code`', '<code class="md-code">code</code>'],
    ['`**not bold**`', '<code class="md-code">**not bold**</code>'],
    ['``with ` tick``', '<code class="md-code">with ` tick</code>'],
    ['`` ` ``', '<code class="md-code">`</code>'],
    ['`a` and `b`', '<code class="md-code">a</code> and <code class="md-code">b</code>'],
    ['`unclosed', '`unclosed'],
    ['``', '``'],
    ['`<b>`', '<code class="md-code">&lt;b&gt;</code>'],
    ['`line\nbreak`', '<code class="md-code">line\nbreak</code>']
  ])
})

describe('headings and subtext', () => {
  table([
    ['# Big', '<h1 class="md-heading md-h1">Big</h1>'],
    ['## Medium', '<h2 class="md-heading md-h2">Medium</h2>'],
    ['### Small', '<h3 class="md-heading md-h3">Small</h3>'],
    ['# **bold** heading', '<h1 class="md-heading md-h1"><strong>bold</strong> heading</h1>'],
    ['#nospace', '#nospace'],
    ['#### too deep', '#### too deep'],
    ['# ', '# '],
    ['text # not a heading', 'text # not a heading'],
    ['intro\n# Head\noutro', 'intro<h1 class="md-heading md-h1">Head</h1>outro'],
    ['-# fine print', '<div class="md-subtext">fine print</div>'],
    ['-#nospace', '-#nospace']
  ])
})

describe('lists', () => {
  const li = (...items: string[]) => items.map(i => `<li>${i}</li>`).join('')
  table([
    ['- a\n- b', `<ul class="md-list">${li('a', 'b')}</ul>`],
    ['* a\n* b', `<ul class="md-list">${li('a', 'b')}</ul>`],
    ['1. a\n2. b', `<ol class="md-list">${li('a', 'b')}</ol>`],
    ['3. a\n4. b', `<ol class="md-list" start="3">${li('a', 'b')}</ol>`],
    ['- **bold** item', `<ul class="md-list">${li('<strong>bold</strong> item')}</ul>`],
    ['- a\n  - b\n    - c\n- d', `<ul class="md-list"><li>a<ul class="md-list"><li>b<ul class="md-list">${li('c')}</ul></li></ul></li><li>d</li></ul>`],
    ['1. a\n  - b', '<ol class="md-list"><li>a<ul class="md-list"><li>b</li></ul></li></ol>'],
    ['- a\n1. b', `<ul class="md-list">${li('a')}</ul><ol class="md-list">${li('b')}</ol>`],
    ['-nospace', '-nospace'],
    ['- ', '- '],
    ['text\n- a', `text<ul class="md-list">${li('a')}</ul>`],
    ['- a\ntext', `<ul class="md-list">${li('a')}</ul>text`],
    ['1.5 liters', '1.5 liters']
  ])

  it('nests no deeper than the limit', () => {
    const deep = Array.from({ length: 20 }, (_, i) => `${' '.repeat(i * 2)}- x`).join('\n')
    expect(renderMarkdown(deep).match(/<ul/g)).toHaveLength(6)
  })
})

describe('quotes', () => {
  const quote = (inner: string) => `<blockquote class="md-quote">${inner}</blockquote>`
  table([
    ['> quote', quote('quote')],
    ['> a\n> b', quote('a\nb')],
    ['> a\nplain', `${quote('a')}plain`],
    ['plain\n> a', `plain${quote('a')}`],
    ['> a\n\n> b', `${quote('a')}${quote('b')}`],
    ['>no space', '&gt;no space'],
    ['> **bold** `code`', quote('<strong>bold</strong> <code class="md-code">code</code>')],
    ['>>> everything\nafter', quote('everything\nafter')],
    ['before\n>>> a\n> b\n# c', `before${quote('a\n&gt; b<h1 class="md-heading md-h1">c</h1>')}`],
    ['> > nested', quote('&gt; nested')],
    ['>>>no space', '&gt;&gt;&gt;no space'],
    ['> - list in quote', quote('<ul class="md-list"><li>list in quote</li></ul>')]
  ])

  it('lets a block quote reach through code blocks to the end of the message', () => {
    const html = renderMarkdown('>>> one\n```\ncode\n```\ntwo')
    expect(html.startsWith('<blockquote class="md-quote">one')).toBe(true)
    expect(html).toContain('md-codeblock')
    expect(html.endsWith('two</blockquote>')).toBe(true)
  })
})

describe('links', () => {
  table([
    ['https://example.com', link('https://example.com')],
    ['see https://example.com/a?b=1&c=2.', `see ${link('https://example.com/a?b=1&amp;c=2')}.`],
    ['(https://example.com)', `(${link('https://example.com')})`],
    ['https://example.com/wiki/A_(b)', link('https://example.com/wiki/A_(b)')],
    ['https://example.com/a_b_c*', `${link('https://example.com/a_b_c')}*`],
    ['**https://example.com/a*b*c**', `<strong>${link('https://example.com/a*b*c')}</strong>`],
    ['||https://example.com||', spoiler(link('https://example.com'))],
    ['__https://example.com__', underline(link('https://example.com'))],
    ['<https://example.com/x>', link('https://example.com/x')],
    ['<mailto:a@example.com>', `<a href="mailto:a@example.com" rel="noopener noreferrer nofollow" class="md-link">mailto:a@example.com</a>`],
    ['[text](https://example.com)', masked('https://example.com', 'example.com', 'text')],
    ['[**bold** text](https://example.com)', masked('https://example.com', 'example.com', '<strong>bold</strong> text')],
    ['[example.com](https://example.com/page)', masked('https://example.com/page', 'example.com', 'example.com', false)],
    ['[https://example.com/a](https://example.com/b)', masked('https://example.com/b', 'example.com', 'https://example.com/a', false)],
    ['[www.example.com](https://example.com)', masked('https://example.com', 'example.com', 'www.example.com', false)],
    ['[example.com.evil.test](https://example.com)', masked('https://example.com', 'example.com', 'example.com.evil.test')],
    ['[x](<https://example.com/a b>)', `[x](&lt;${link('https://example.com/a')} b&gt;)`],
    ['[x](https://example.com "title")', masked('https://example.com', 'example.com', 'x')],
    ['[x](https://example.com/a_(b))', masked('https://example.com/a_(b)', 'example.com', 'x')],
    ['[a@example.com](mailto:a@example.com)', `<a href="mailto:a@example.com" rel="noopener noreferrer nofollow" class="md-link md-link-masked" title="mailto:a@example.com" data-host="a@example.com">a@example.com</a>`],
    ['[https://a.test](https://b.test)', masked('https://b.test', 'b.test', 'https://a.test')],
    ['[[nested]](https://example.com)', masked('https://example.com', 'example.com', '[nested]')],
    ['[no url]', '[no url]'],
    ['[empty]()', '[empty]()'],
    ['[](https://example.com)', '[](' + link('https://example.com') + ')'],
    ['[a](https://example.com', `[a](${link('https://example.com')}`],
    ['text[x](https://example.com)text', `text${masked('https://example.com', 'example.com', 'x')}text`]
  ])

  it('does not link inside a link label', () => {
    expect(renderMarkdown('[see https://a.test](https://b.test)')).toBe(masked('https://b.test', 'b.test', 'see https://a.test'))
  })

  it('refuses every scheme except http, https and mailto', () => {
    for (const href of ['javascript:alert(1)', 'JaVaScRiPt:alert(1)', 'data:text/html;base64,AAAA', 'vbscript:x', 'file:///etc/passwd', 'ftp://example.com', '//example.com', '/relative', 'tel:123', 'ws://example.com/x']) {
      const html = renderMarkdown(`[x](${href}) <${href}>`)
      expect(html).not.toContain('<a ')
      expect(html).not.toContain('href=')
    }
  })
})

describe('line breaks and paragraphs', () => {
  table([
    ['a\nb', 'a\nb'],
    ['a\n\nb', 'a\n\nb'],
    ['a\n', 'a\n'],
    ['', ''],
    ['   ', '   ']
  ])
})

describe('plain text for previews', () => {
  it.each([
    ['**bold** *it* __u__ ~~s~~', 'bold it u s'],
    ['||secret||', 'Spoiler'],
    ['`code` and ```\nblock\n```', 'code and \nblock'],
    ['# Heading\n-# small\n- item\n1. num\n> quote', 'Heading\nsmall\nitem\nnum\nquote'],
    ['[text](https://example.com)', 'text'],
    ['<https://example.com>', 'https://example.com'],
    ['https://example.com/a.', 'https://example.com/a.'],
    ['\\*kept\\*', '*kept*'],
    ['>>> all\nof it', 'all\nof it']
  ])('%j', (input, plain) => {
    expect(markdownToPlain(input)).toBe(plain)
  })

  it('agrees with the rendered text for formatting without controls', () => {
    for (const md of ['**a** *b*', '~~x~~ __y__', 'a `b` c', 'https://example.com/a.']) {
      const el = document.createElement('div')
      el.innerHTML = renderMarkdown(md)
      expect(markdownToPlain(md).replace(/\s+/g, ' ').trim()).toBe((el.textContent ?? '').replace(/\s+/g, ' ').trim())
    }
  })
})
