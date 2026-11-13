// The in-page half of the clipping audit (clipping.spec.ts). auditClipping is
// handed to page.evaluate as it is, so it may not use anything from outside its
// own body: every helper is declared inside it.

export type ClipRule =
  | 'overflow-hidden' // content larger than a box that hides overflow
  | 'truncated-without-full-text' // an ellipsis or line clamp with no title, label or tooltip that holds the whole text
  | 'outside-viewport' // partly outside the window and not reachable by scrolling
  | 'outside-clip' // partly outside an ancestor that clips
  | 'covered' // an interactive element whose centre another element covers
  | 'text-cut' // the glyphs of a line reach past the box that clips them

export interface ClipFinding {
  rule: ClipRule
  selector: string
  text: string
  /** What was measured, in CSS pixels. */
  measured: Record<string, number | string>
  rect: { x: number; y: number; width: number; height: number }
}

export interface AuditOptions {
  /** Pixels of tolerance for rounding (sub-pixel layout, borders). */
  tolerance: number
}

export function auditClipping(options: AuditOptions): ClipFinding[] {
  const tol = options.tolerance
  const findings: ClipFinding[] = []
  const vw = document.documentElement.clientWidth
  const vh = document.documentElement.clientHeight

  const INTERACTIVE = 'button, a[href], input:not([type="hidden"]), select, textarea, summary, [role="button"], [role="menuitem"], [role="tab"], [role="option"], [role="switch"], [role="checkbox"], [tabindex]:not([tabindex="-1"])'
  const LEAF = 'button, a, input, select, textarea, img, svg, video, canvas, label, h1, h2, h3, h4, p, span, li, td, th, code, kbd, time, [role="img"]'

  // Computed styles are live objects; one per element is enough for a whole audit.
  const styles = new Map<Element, CSSStyleDeclaration>()
  const styleOf = (el: Element) => {
    let s = styles.get(el)
    if (!s) { s = getComputedStyle(el); styles.set(el, s) }
    return s
  }
  const clips = (value: string) => value === 'hidden' || value === 'clip'
  const scrolls = (value: string) => value === 'auto' || value === 'scroll'
  const round = (n: number) => Math.round(n * 10) / 10

  function cssEscape(value: string): string {
    return typeof CSS !== 'undefined' && CSS.escape ? CSS.escape(value) : value.replace(/["\\]/g, '\\$&')
  }

  /** A selector that survives re-renders: the nearest test id, id or label as anchor, then tag:nth-of-type steps. */
  function selectorOf(el: Element): string {
    const steps: string[] = []
    let node: Element | null = el
    while (node && node !== document.body && steps.length < 8) {
      const testid = node.getAttribute('data-testid')
      if (testid) { steps.unshift(`[data-testid="${cssEscape(testid)}"]`); break }
      if (node.id && !/^\d|[:]/.test(node.id) && !/^mnema-tooltip/.test(node.id)) { steps.unshift(`#${cssEscape(node.id)}`); break }
      const label = node.getAttribute('aria-label')
      const tag = node.tagName.toLowerCase()
      if (label && label.length <= 40 && ['nav', 'section', 'aside', 'main', 'dialog', 'div'].includes(tag)) { steps.unshift(`${tag}[aria-label="${cssEscape(label)}"]`); break }
      const parent: Element | null = node.parentElement
      const same = parent ? [...parent.children].filter(c => c.tagName === node?.tagName) : []
      steps.unshift(same.length > 1 ? `${tag}:nth-of-type(${same.indexOf(node) + 1})` : tag)
      node = parent
    }
    return steps.join(' > ')
  }

  function rectOf(el: Element) {
    const r = el.getBoundingClientRect()
    return { x: round(r.x), y: round(r.y), width: round(r.width), height: round(r.height) }
  }

  function textOf(el: Element): string {
    return (el.textContent ?? '').replace(/\s+/g, ' ').trim()
  }

  /** Not shown, or deliberately out of reach: inert, aria-hidden, screen-reader only, invisible. */
  function ignored(el: Element): boolean {
    // aria-hidden on an icon only hides it from screen readers; on a container it marks a closed panel.
    if (el.closest('[inert], [aria-hidden="true"]:not(svg), .sr-only, .mnema-tooltip')) return true
    const s = styleOf(el)
    // No box of its own (display: contents) or not painted; its children are checked one by one.
    if (s.visibility !== 'visible' || s.display === 'contents' || s.display === 'none') return true
    const r = el.getBoundingClientRect()
    if (r.width < 1 || r.height < 1) return true
    // An element whose own box is cut to a pixel (the sr-only pattern without the class).
    return s.position === 'absolute' && r.width <= 1 && r.height <= 1
  }

  const hiddenMemo = new Map<Element, boolean>()
  /** Not painted: itself or an ancestor is transparent, hidden or not displayed. */
  function hiddenByOpacity(el: Element): boolean {
    const known = hiddenMemo.get(el)
    if (known !== undefined) return known
    const s = styleOf(el)
    const parent = el.parentElement
    const hidden = Number(s.opacity) === 0 || s.visibility === 'hidden' || s.display === 'none' || (parent !== null && hiddenByOpacity(parent))
    hiddenMemo.set(el, hidden)
    return hidden
  }

  /** The whole text is available some other way: a title, an aria-label or a v-tooltip that contains it. */
  function hasFullText(el: Element): boolean {
    const full = textOf(el).toLowerCase()
    if (!full) return true
    // v-full-text (web/src/directives/fullText.ts) puts the whole text in a title while it is cut.
    if (el.hasAttribute('data-full-text')) return true
    let node: Element | null = el
    for (let depth = 0; node && depth < 5; depth++, node = node.parentElement) {
      const tip = (node as Element & { __tip?: { text?: string } | null }).__tip?.text
      for (const candidate of [node.getAttribute('title'), node.getAttribute('aria-label'), tip]) {
        if (candidate && candidate.replace(/\s+/g, ' ').trim().toLowerCase().includes(full)) return true
      }
      if (node.matches(INTERACTIVE)) break
    }
    return false
  }

  function deliberateTruncation(el: Element, s: CSSStyleDeclaration, axis: 'x' | 'y'): boolean {
    if (axis === 'x') return s.textOverflow === 'ellipsis'
    const clamp = s.getPropertyValue('-webkit-line-clamp') || s.getPropertyValue('line-clamp')
    return clamp !== '' && clamp !== 'none'
  }

  /**
   * How far the shown content reaches past the box. Walks the subtree once:
   * a child that is not shown is skipped with everything in it, and a child
   * that clips or scrolls on the axis counts with its own box only, because
   * what it hides is its own business.
   */
  function visibleOverflow(el: Element, axis: 'x' | 'y'): number {
    const box = el.getBoundingClientRect()
    const past = (r: DOMRect) => axis === 'x' ? Math.max(box.left - r.left, r.right - box.right) : Math.max(box.top - r.top, r.bottom - box.bottom)
    const range = document.createRange()
    let worst = 0
    const visit = (node: Element) => {
      for (const child of node.childNodes) {
        if (child.nodeType === Node.TEXT_NODE) {
          if (!child.textContent?.trim()) continue
          range.selectNodeContents(child)
          for (const r of range.getClientRects()) if (r.width >= 1) worst = Math.max(worst, past(r))
          continue
        }
        if (!(child instanceof Element)) continue
        const s = styleOf(child)
        if (s.display === 'none' || s.position === 'fixed' || Number(s.opacity) === 0 || child.matches('[inert], .sr-only, .mnema-tooltip, [aria-hidden="true"]:not(svg)')) continue
        const r = child.getBoundingClientRect()
        if (s.visibility === 'visible' && r.width >= 1 && r.height >= 1) worst = Math.max(worst, past(r))
        if ((axis === 'x' ? s.overflowX : s.overflowY) === 'visible') visit(child)
      }
    }
    visit(el)
    return worst
  }

  function checkOverflow(el: Element) {
    const s = styleOf(el)
    for (const axis of ['x', 'y'] as const) {
      const overflow = axis === 'x' ? s.overflowX : s.overflowY
      if (!clips(overflow)) continue
      const scrollSize = axis === 'x' ? el.scrollWidth : el.scrollHeight
      const clientSize = axis === 'x' ? el.clientWidth : el.clientHeight
      if (scrollSize - clientSize <= tol) continue
      if (deliberateTruncation(el, s, axis)) {
        if (!hasFullText(el)) findings.push({ rule: 'truncated-without-full-text', selector: selectorOf(el), text: textOf(el).slice(0, 80), measured: { axis, scrollSize, clientSize }, rect: rectOf(el) })
        continue
      }
      const visible = visibleOverflow(el, axis)
      if (visible <= tol) continue
      findings.push({ rule: 'overflow-hidden', selector: selectorOf(el), text: textOf(el).slice(0, 80), measured: { axis, scrollSize, clientSize, visibleOverflow: round(visible) }, rect: rectOf(el) })
    }
  }

  /** The nearest ancestor that clips or scrolls on an axis, with what it does there. */
  function clipAncestors(el: Element): Array<{ el: Element; x: string; y: string }> {
    const out: Array<{ el: Element; x: string; y: string }> = []
    for (let node = el.parentElement; node && node !== document.documentElement; node = node.parentElement) {
      const s = styleOf(node)
      if (s.overflowX !== 'visible' || s.overflowY !== 'visible') out.push({ el: node, x: s.overflowX, y: s.overflowY })
      if (s.position === 'fixed') break
    }
    return out
  }

  function checkPlacement(el: Element) {
    const r = el.getBoundingClientRect()
    const ancestors = clipAncestors(el)
    for (const a of ancestors) {
      const box = a.el.getBoundingClientRect()
      const outX = Math.max(box.left - r.left, r.right - box.right)
      const outY = Math.max(box.top - r.top, r.bottom - box.bottom)
      const fullyOut = r.right <= box.left || r.left >= box.right || r.bottom <= box.top || r.top >= box.bottom
      if (fullyOut) return // scrolled away or a closed panel: not a partial cut
      const cutX = outX > tol && clips(a.x)
      const cutY = outY > tol && clips(a.y)
      if (cutX || cutY) {
        findings.push({ rule: 'outside-clip', selector: selectorOf(el), text: textOf(el).slice(0, 80), measured: { by: selectorOf(a.el), outX: round(outX), outY: round(outY) }, rect: rectOf(el) })
        return
      }
      if (scrolls(a.x) || scrolls(a.y)) return // the scroll container brings it into view
    }
    const docScrolls = document.documentElement.scrollHeight > vh + tol && !clips(styleOf(document.documentElement).overflowY) && !clips(styleOf(document.body).overflowY)
    const outX = Math.max(-r.left, r.right - vw)
    const outY = docScrolls ? 0 : Math.max(-r.top, r.bottom - vh)
    const fullyOut = r.right <= 0 || r.left >= vw || r.bottom <= 0 || r.top >= vh
    if (!fullyOut && (outX > tol || outY > tol)) {
      findings.push({ rule: 'outside-viewport', selector: selectorOf(el), text: textOf(el).slice(0, 80), measured: { outX: round(outX), outY: round(outY), viewport: `${vw}x${vh}` }, rect: rectOf(el) })
    }
  }

  /** The point is inside every ancestor that clips or scrolls, so a hit test there is meaningful. */
  function pointShown(el: Element, x: number, y: number): boolean {
    if (x < 0 || y < 0 || x >= vw || y >= vh) return false
    for (const a of clipAncestors(el)) {
      const box = a.el.getBoundingClientRect()
      if (x < box.left || x > box.right || y < box.top || y > box.bottom) return false
    }
    return true
  }

  /**
   * The hit belongs to a layer above the page that does not hold the element:
   * a menu, popover, dialog, toast, drawer or full-screen panel. Those cover
   * what is under them by design; a defect is an overlap within one layer.
   */
  function overlay(hit: Element, el: Element): boolean {
    for (let node: Element | null = hit; node && !node.contains(el); node = node.parentElement) {
      const s = styleOf(node)
      if (s.position === 'fixed' || s.position === 'sticky') return true
      if (node.matches('[role="menu"], [role="dialog"], [role="alertdialog"], [role="listbox"], [role="tooltip"], [data-toast]')) return true
      if (s.position === 'absolute' && Number(s.zIndex) >= 10) return true
    }
    return false
  }

  function checkCovered(el: Element) {
    const r = el.getBoundingClientRect()
    const x = r.left + r.width / 2
    const y = r.top + r.height / 2
    if (!pointShown(el, x, y)) return
    const hit = document.elementFromPoint(x, y)
    if (!hit || hit === el || el.contains(hit) || hit.contains(el)) return
    // A label that forwards to its control, or a control styled through its label.
    if (hit instanceof HTMLLabelElement && (hit.control === el || hit.contains(el))) return
    if (el instanceof HTMLInputElement && el.labels && [...el.labels].some(l => l.contains(hit))) return
    if (hit.closest('.mnema-tooltip') || overlay(hit, el)) return
    findings.push({ rule: 'covered', selector: selectorOf(el), text: textOf(el).slice(0, 80), measured: { by: selectorOf(hit), byText: textOf(hit).slice(0, 40), x: round(x), y: round(y) }, rect: rectOf(el) })
  }

  /** Text nodes whose glyph boxes reach past the nearest box that clips them vertically. */
  function checkTextCut(root: Element) {
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
    const range = document.createRange()
    const seen = new Set<Element>()
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const parent = node.parentElement
      if (!parent || seen.has(parent) || !node.textContent?.trim() || ignored(parent) || hiddenByOpacity(parent)) continue
      // The nearest box that does something with vertical overflow: a scroll container brings the line into view, a clipping one cuts it.
      const nearest = styleOf(parent).overflowY !== 'visible' ? parent : clipAncestors(parent).find(a => a.y !== 'visible')?.el
      if (!nearest || !clips(styleOf(nearest).overflowY)) continue
      const own = nearest
      const box = own.getBoundingClientRect()
      range.selectNodeContents(node)
      for (const r of range.getClientRects()) {
        if (r.width < 1 || r.bottom <= box.top || r.top >= box.bottom) continue
        const cut = Math.max(box.top - r.top, r.bottom - box.bottom)
        // Some glyph boxes are cut but the line is still mostly shown: ascenders or descenders are lost.
        if (cut > tol && cut < r.height * 0.6) {
          seen.add(parent)
          findings.push({ rule: 'text-cut', selector: selectorOf(parent), text: textOf(parent).slice(0, 80), measured: { lineHeight: round(r.height), boxHeight: round(box.height), cut: round(cut), by: selectorOf(own) }, rect: rectOf(parent) })
          break
        }
      }
    }
  }

  /** With a modal dialog open, everything behind it is out of reach on purpose: audit the top dialog only. */
  function auditRoot(): Element {
    const modals = [...document.querySelectorAll('[aria-modal="true"], dialog[open]')].filter(d => !ignored(d))
    return modals.at(-1) ?? document.body
  }

  const root = auditRoot()
  const all = [root, ...root.querySelectorAll('*')].filter(el => !ignored(el) && !hiddenByOpacity(el))
  for (const el of all) checkOverflow(el)
  for (const el of all) if (el.matches(LEAF)) checkPlacement(el)
  for (const el of all) if (el.matches(INTERACTIVE)) checkCovered(el)
  checkTextCut(root)
  return dedupe(findings)

  function dedupe(list: ClipFinding[]): ClipFinding[] {
    const keys = new Set<string>()
    return list.filter(f => {
      const key = `${f.rule} ${f.selector}`
      if (keys.has(key)) return false
      keys.add(key)
      return true
    })
  }
}
