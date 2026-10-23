// Sliders paint the part up to the thumb through the --fill custom property
// (see styles/controls.css). It is set through the CSSOM, which the page's
// style-src-attr 'none' policy allows, and kept current on input and when a
// slider is added to the page.

export function paintRange(input: HTMLInputElement): void {
  const min = input.min === '' ? 0 : Number(input.min)
  const max = input.max === '' ? 100 : Number(input.max)
  const span = max - min
  const share = span > 0 ? (Number(input.value) - min) / span : 0
  const percent = Math.min(100, Math.max(0, share * 100))
  input.style.setProperty('--fill', `${percent}%`)
}

function isRange(node: unknown): node is HTMLInputElement {
  return node instanceof HTMLInputElement && node.type === 'range'
}

function paintWithin(root: ParentNode): void {
  root.querySelectorAll<HTMLInputElement>('input[type="range"]').forEach(paintRange)
}

/** Keeps every slider of the document painted; returns a function that stops it. */
export function installRangeFill(doc: Document = document): () => void {
  // A slider whose value Vue set after it was added is repainted when it is first pointed at.
  const repaint = (event: Event): void => {
    if (isRange(event.target)) paintRange(event.target)
  }
  const observer = new MutationObserver((records) => {
    for (const record of records) {
      for (const node of record.addedNodes) {
        if (isRange(node)) paintRange(node)
        else if (node instanceof Element) paintWithin(node)
      }
    }
  })
  const events = ['input', 'change', 'pointerover', 'focusin']
  for (const name of events) doc.addEventListener(name, repaint, true)
  observer.observe(doc.documentElement, { childList: true, subtree: true })
  paintWithin(doc)
  return () => {
    for (const name of events) doc.removeEventListener(name, repaint, true)
    observer.disconnect()
  }
}
