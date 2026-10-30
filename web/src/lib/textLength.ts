// Character counting that agrees with the server. The server counts Unicode
// code points (Go runes), but String.length and the maxlength attribute count
// UTF-16 units: one emoji is 2 of them, so a limit of 24 would stop at 12
// emoji and cut a pasted text in the middle of a surrogate pair. Everything
// here counts code points and never cuts through a character the reader sees
// (a ZWJ sequence, a skin tone, a flag or a letter with its combining marks).

const ZWJ = 0x200d
const REGIONAL_FIRST = 0x1f1e6
const REGIONAL_LAST = 0x1f1ff

/** Counts code points, which is how the server measures a limit. */
export function charCount(text: string): number {
  let n = 0
  for (let i = 0; i < text.length; i++) {
    const unit = text.charCodeAt(i)
    if (unit >= 0xd800 && unit <= 0xdbff && i + 1 < text.length) {
      const low = text.charCodeAt(i + 1)
      if (low >= 0xdc00 && low <= 0xdfff) i++
    }
    n++
  }
  return n
}

const mark = /^\p{M}$/u

/** Characters that attach to the one before: marks, joiner, variation selectors, skin tones, tag characters. */
function attaches(point: string): boolean {
  const cp = point.codePointAt(0)!
  return cp === ZWJ
    || (cp >= 0xfe00 && cp <= 0xfe0f)
    || (cp >= 0x1f3fb && cp <= 0x1f3ff)
    || (cp >= 0xe0020 && cp <= 0xe007f)
    || mark.test(point)
}

function isRegional(cp: number | undefined): boolean {
  return cp !== undefined && cp >= REGIONAL_FIRST && cp <= REGIONAL_LAST
}

/** Cuts text to at most max code points, backed up so no character is split. */
export function clampChars(text: string, max: number): string {
  if (max <= 0) return ''
  const points = Array.from(text)
  if (points.length <= max) return text
  let cut = max
  while (cut > 0) {
    const next = points[cut]!
    const prev = points[cut - 1]!
    if (attaches(next) || prev.codePointAt(0) === ZWJ) {
      cut--
    } else if (isRegional(next.codePointAt(0)) && isRegional(prev.codePointAt(0)) && regionalRun(points, cut) % 2 === 1) {
      cut--
    } else {
      break
    }
  }
  return points.slice(0, cut).join('')
}

function regionalRun(points: string[], end: number): number {
  let n = 0
  for (let i = end - 1; i >= 0 && isRegional(points[i]!.codePointAt(0)); i--) n++
  return n
}
