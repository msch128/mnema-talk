/** The first character of a name, upper-cased, for an avatar without a picture ('?' for no name). Whole code points: an emoji is never cut in half. */
export function initialOf(name: string | null | undefined): string {
  return Array.from((name ?? '').trim())[0]?.toUpperCase() || '?'
}
