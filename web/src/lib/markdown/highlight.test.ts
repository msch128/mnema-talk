import { describe, expect, it } from 'vitest'
import { highlightCode, sanitizeHighlighted } from './highlight'

describe('sanitizeHighlighted', () => {
  it('keeps the spans highlight.js puts around embedded languages', () => {
    const html = '<span class="hljs-tag">&lt;<span class="hljs-name">script</span>&gt;</span><span class="language-javascript">x</span>'
    expect(sanitizeHighlighted(html)).toBe(html)
  })

  it('rejects other markup and unbalanced spans', () => {
    expect(sanitizeHighlighted('<span class="language-js">x')).toBeNull()
    expect(sanitizeHighlighted('x</span>')).toBeNull()
    expect(sanitizeHighlighted('<span class="evil">x</span>')).toBeNull()
    expect(sanitizeHighlighted('<b>x</b>')).toBe('&lt;b&gt;x&lt;/b&gt;')
  })
})

describe('highlightCode', () => {
  it('highlights html with an embedded script and style', () => {
    const out = highlightCode('<p>a</p>\n<script>const x = 1</script>\n<style>a { color: red }</style>', 'html')
    expect(out).not.toBeNull()
    expect(out).toContain('language-javascript')
    expect(out).toContain('hljs-keyword')
  })
})
