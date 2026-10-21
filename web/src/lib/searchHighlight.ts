// Splits text into plain and matching segments for highlighting. The result is
// rendered as text nodes (never HTML), so message content cannot inject markup.

export interface HighlightPart { text: string; match: boolean }

/** Case-insensitive segments of text matching any of the terms. */
export function splitHighlight(text: string, terms: string[]): HighlightPart[] {
  const needles = terms.map(t => t.toLowerCase()).filter(Boolean)
  if (!text || !needles.length) return text ? [{ text, match: false }] : []
  const lower = text.toLowerCase()
  const mask = new Array<boolean>(text.length).fill(false)
  for (const needle of needles) {
    // toLowerCase can change the length (e.g. "İ"); then offsets no longer line up.
    if (lower.length !== text.length) return [{ text, match: false }]
    for (let at = lower.indexOf(needle); at !== -1; at = lower.indexOf(needle, at + needle.length)) {
      for (let i = at; i < at + needle.length; i++) mask[i] = true
    }
  }
  const parts: HighlightPart[] = []
  let start = 0
  for (let i = 1; i <= text.length; i++) {
    if (i === text.length || mask[i] !== mask[start]) {
      parts.push({ text: text.slice(start, i), match: !!mask[start] })
      start = i
    }
  }
  return parts
}
