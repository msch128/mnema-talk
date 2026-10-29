// Game executable names as the desktop host accepts them (valid_executable
// there): a basename of 5 to 128 characters ending in .exe, starting with a
// letter or digit, then letters, digits and "-_. !", and no "..". At most 32.
export const MAX_GAMES = 32

export type GameNameProblem = 'empty' | 'invalid' | 'duplicate' | 'full'

// A pasted path is reduced to its file name, so "D:\Games\Wow.exe" works.
export function baseName(raw: string): string {
  const trimmed = raw.trim()
  return trimmed.slice(Math.max(trimmed.lastIndexOf('\\'), trimmed.lastIndexOf('/')) + 1).trim()
}

export function isExecutableName(name: string): boolean {
  return name.length > 4 && name.length <= 128 && /^[A-Za-z0-9][A-Za-z0-9_. !-]*$/.test(name) && /\.exe$/i.test(name) && !name.includes('..')
}

// Windows file names are case-insensitive, so the duplicate check is as well.
export function checkGameName(raw: string, existing: readonly string[]): { name: string } | { problem: GameNameProblem } {
  const name = baseName(raw)
  if (!name) return { problem: 'empty' }
  if (!isExecutableName(name)) return { problem: 'invalid' }
  if (existing.some(item => item.toLowerCase() === name.toLowerCase())) return { problem: 'duplicate' }
  if (existing.length >= MAX_GAMES) return { problem: 'full' }
  return { name }
}
