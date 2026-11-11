// [[Page title]] and [[Page title|shown text]] in a wiki page: a link to
// another page, found by title or address when the page is shown, so it follows
// a rename. A name that matches no page stays as text with a dotted underline.
import type { TextRule } from '../../shared/lib/markdown'
import type { WikiPageSummary } from './wikiApi'
import { resolveWikiLink } from './wikiTree'

const group = (m: RegExpMatchArray, index: number): string => m[index] ?? ''

/** The rule for the pages the reader can see. Linear: no nested quantifiers. */
export function wikiLinkRule(pages: readonly WikiPageSummary[]): TextRule {
  return {
    re: /\[\[([^[\]|\n]{1,100})(?:\|([^[\]\n]{1,100}))?\]\]/g,
    build(m) {
      const target = group(m, 1).trim()
      const label = (m[2] === undefined ? target : group(m, 2)).trim() || target
      const slug = resolveWikiLink(pages, target)
      if (!slug) return [{ t: 'el', tag: 'span', attrs: { class: 'md-wikilink md-wikilink-missing' }, c: [{ t: 'text', v: label }] }]
      return [{ t: 'el', tag: 'span', attrs: { class: 'md-wikilink', 'data-wiki': slug, role: 'button', tabindex: '0' }, c: [{ t: 'text', v: label }] }]
    },
    plain: m => (m[2] === undefined ? group(m, 1) : group(m, 2)).trim()
  }
}

const MEDIA_ID = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'

/**
 * `![alt](/api/media/<id>)`: a picture uploaded to the wiki. Only that address
 * shape becomes an image (the renderer allows no other source); anything else
 * stays text. Linear: no nested quantifiers.
 */
export const wikiImageRule: TextRule = {
  re: new RegExp(`!\\[([^\\[\\]\\n]{0,100})\\]\\((/api/media/${MEDIA_ID})\\)`, 'g'),
  build(m) {
    const alt = group(m, 1).trim()
    return [{ t: 'el', tag: 'img', attrs: { src: group(m, 2), alt, class: 'md-wikiimage', loading: 'lazy', draggable: 'false' }, c: [] }]
  },
  plain: m => group(m, 1).trim()
}
