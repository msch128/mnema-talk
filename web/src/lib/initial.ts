// First character of a name for avatar fallbacks. Takes a whole code point, so
// an emoji or other astral character is not cut into a lone surrogate.
export function nameInitial(name: string): string {
  return (Array.from(name)[0] ?? '?').toUpperCase()
}
