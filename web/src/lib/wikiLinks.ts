// [[Page title]] and [[Page title|shown text]] in a wiki page: a link to
// another page, found by title or address when the page is shown, so it follows
// a rename. A name that matches no page stays as text with a dotted underline.
import type { TextRule } from './markdown'
import type { WikiPageSummary } from './wikiApi'
import { resolveWikiLink } from './wikiTree'

/** The rule for the pages the reader can see. Linear: no nested quantifiers. */
export function wikiLinkRule(pages: readonly WikiPageSummary[]): TextRule {
  return {
    re: /\[\[([^[\]|\n]{1,100})(?:\|([^[\]\n]{1,100}))?\]\]/g,
    build(m) {
      const target = (m[1] ?? '').trim()
      const label = (m[2] ?? target).trim() || target
      const slug = resolveWikiLink(pages, target)
      if (!slug) return [{ t: 'el', tag: 'span', attrs: { class: 'md-wikilink md-wikilink-missing' }, c: [{ t: 'text', v: label }] }]
      return [{ t: 'el', tag: 'span', attrs: { class: 'md-wikilink', 'data-wiki': slug, role: 'button', tabindex: '0' }, c: [{ t: 'text', v: label }] }]
    },
    plain: m => (m[2] ?? m[1] ?? '').trim()
  }
}
