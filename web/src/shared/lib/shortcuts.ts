// The keyboard shortcuts the app really has, for the cheat sheet. Add a row
// here when a shortcut is added (and its label under "shortcuts" in the
// language files); a key that is not handled somewhere is not listed.
import { t } from '../../i18n'

/** A key as shown: "mod" is Ctrl, or ⌘ on a Mac; anything else is the key itself. */
export type KeyName = string

/** One way to press a shortcut; a row with several (Ctrl+/ and ?) lists them with "or". */
export type KeyCombo = readonly KeyName[]

export interface Shortcut {
  id: string
  combos: readonly KeyCombo[]
}

export interface ShortcutGroup {
  id: 'general' | 'messages' | 'composer' | 'talk' | 'sidebar'
  /** The group has a line under its heading (shortcuts.groupHint.<id>) saying where the keys work. */
  hint?: true
  items: readonly Shortcut[]
}

export const SHORTCUT_GROUPS: readonly ShortcutGroup[] = [
  {
    id: 'general',
    items: [
      { id: 'search', combos: [['mod', 'K']] },
      { id: 'shortcuts', combos: [['mod', '/'], ['?']] },
      { id: 'escape', combos: [['Esc']] }
    ]
  },
  {
    id: 'messages',
    hint: true,
    items: [
      { id: 'move', combos: [['↑'], ['↓']] },
      { id: 'reply', combos: [['R']] },
      { id: 'edit', combos: [['E']] },
      { id: 'save', combos: [['S']] },
      { id: 'menu', combos: [['Menu'], ['Shift', 'F10']] }
    ]
  },
  {
    id: 'composer',
    items: [
      { id: 'send', combos: [['Enter']] },
      { id: 'newline', combos: [['Shift', 'Enter']] },
      { id: 'mention', combos: [['@']] },
      { id: 'command', combos: [['/']] },
      { id: 'emoji', combos: [[':']] },
      { id: 'cancelReply', combos: [['Esc']] }
    ]
  },
  {
    id: 'talk',
    items: [
      { id: 'fullscreen', combos: [['F']] },
      // The key is the member's own choice; the dialog fills it in.
      { id: 'ptt', combos: [] }
    ]
  },
  {
    id: 'sidebar',
    hint: true,
    items: [{ id: 'reorder', combos: [['Alt', '↑'], ['Alt', '↓']] }]
  }
]

const MAC_SYMBOLS: Readonly<Record<string, string>> = { mod: '⌘', Alt: '⌥', Shift: '⇧', Ctrl: '⌃' }
const NAMED_KEYS = new Set(['mod', 'Alt', 'Shift', 'Ctrl', 'Space'])

/** How a key is written on this machine: ⌘ ⌥ ⇧ on a Mac, the localised word (Ctrl/Strg) elsewhere. */
export function keyLabel(key: KeyName, mac: boolean, translate: typeof t = t): string {
  if (mac && key in MAC_SYMBOLS) return MAC_SYMBOLS[key] as string
  return NAMED_KEYS.has(key) ? translate(`shortcuts.key.${key}`) : key
}

/** A whole combo as one phrase: "⌘K" on a Mac, "Ctrl+K" / "Strg+K" elsewhere. */
export function formatShortcut(keys: KeyCombo, mac: boolean = isMac(), translate: typeof t = t): string {
  return keys.map(k => keyLabel(k, mac, translate)).join(mac ? '' : '+')
}

export function isMac(): boolean {
  return typeof navigator !== 'undefined' && /mac|iphone|ipad/i.test(navigator.platform || navigator.userAgent)
}
