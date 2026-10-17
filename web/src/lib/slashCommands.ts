// Slash commands of bots: what the composer offers while "/" is typed, and how
// the values of a picked command become the options the server expects
// (doc/bots.md). Pure functions; the composable wires them to the composer.
import type { BotChannelCommand } from '../types/rest'

/** Option types, with Discord's numbers. */
export const OPTION = { string: 3, integer: 4, boolean: 5, user: 6, channel: 7 } as const

const MAX_SUGGESTIONS = 8

/** What follows a leading "/" while the first word is still being typed; null
 * when the text is no command (it has to start with "/", have no space yet). */
export function slashQuery(text: string): string | null {
  if (!text.startsWith('/') || /\s/.test(text)) return null
  return text.slice(1).toLowerCase()
}

/** Commands whose name starts with the query first, then those containing it. */
export function filterCommands(list: readonly BotChannelCommand[], query: string): BotChannelCommand[] {
  const starts = list.filter(c => c.name.startsWith(query))
  const contains = list.filter(c => !c.name.startsWith(query) && c.name.includes(query))
  return [...starts, ...contains].slice(0, MAX_SUGGESTIONS)
}

export type OptionValues = Record<string, string | boolean>

/** An empty value for every option (booleans start off). */
export function emptyValues(command: Pick<BotChannelCommand, 'options'>): OptionValues {
  const out: OptionValues = {}
  for (const o of command.options) out[o.name] = o.type === OPTION.boolean ? false : ''
  return out
}

export interface BuiltOptions {
  options: Record<string, string | number | boolean>
  /** The first required option without a value, or an integer that is none. */
  problem: { name: string; kind: 'missing' | 'integer' } | null
}

/** The options to send: empty optional ones are left out; integers are numbers. */
export function buildOptions(command: Pick<BotChannelCommand, 'options'>, values: OptionValues): BuiltOptions {
  const options: Record<string, string | number | boolean> = {}
  for (const o of command.options) {
    const raw = values[o.name]
    if (o.type === OPTION.boolean) {
      options[o.name] = raw === true
      continue
    }
    const text = typeof raw === 'string' ? raw.trim() : ''
    if (text === '') {
      if (o.required) return { options, problem: { name: o.name, kind: 'missing' } }
      continue
    }
    if (o.type === OPTION.integer) {
      const n = Number(text)
      if (!Number.isSafeInteger(n)) return { options, problem: { name: o.name, kind: 'integer' } }
      options[o.name] = n
      continue
    }
    options[o.name] = text
  }
  return { options, problem: null }
}
