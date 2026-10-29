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
  if (code in NAMES) return NAMES[code] ?? code
  const letter = /^Key([A-Z])$/.exec(code)
  if (letter) return letter[1] ?? code
  const digit = /^(?:Digit|Numpad)(\d)$/.exec(code)
  if (digit) return code.startsWith('Numpad') ? `Num ${digit[1]}` : (digit[1] ?? code)
  const arrow = /^Arrow(\w+)$/.exec(code)
  if (arrow) return arrow[1] ?? code
  return code.replace(/([a-z])([A-Z])/g, '$1 $2')
}
