// How a push-to-talk key is shown: the physical key code ("KeyV", "F8",
// "Backquote") as the word a member would use.

const NAMES: Record<string, string> = {
  Space: 'Space',
  Backquote: '`',
  Minus: '-',
  Equal: '=',
  BracketLeft: '[',
  BracketRight: ']',
  Backslash: '\\',
  Semicolon: ';',
  Quote: "'",
  Comma: ',',
  Period: '.',
  Slash: '/',
  Escape: 'Esc',
  Backspace: 'Backspace',
  Enter: 'Enter',
  Tab: 'Tab',
  CapsLock: 'Caps Lock'
}

export function pttKeyLabel(code: string): string {
  const named = NAMES[code]
  if (named) return named
  const [, letter] = /^Key([A-Z])$/.exec(code) ?? []
  if (letter) return letter
  const [, numpad, digit] = /^(Numpad|Digit)(\d)$/.exec(code) ?? []
  if (digit) return numpad === 'Numpad' ? `Num ${digit}` : digit
  const [, arrow] = /^Arrow(\w+)$/.exec(code) ?? []
  if (arrow) return arrow
  return code.replace(/([a-z])([A-Z])/g, '$1 $2')
}
