// Message components of bots (doc/bots.md, Message components): action rows of
// buttons and select menus. The server validated them when the bot posted; this
// reads them defensively, decides what a click sends and how a refusal reads.
import type { ChatActionRow, ChatComponent } from '../../types/rest'
import { isApiError, caughtErrorMessage } from '../../shared/lib/api'
import { t } from '../../i18n'

export type Component = ChatComponent
export type ActionRow = ChatActionRow

export const BUTTON = 2
export const STRING_SELECT = 3
export const TEXT_INPUT = 4
export const USER_SELECT = 5
export const ROLE_SELECT = 6
export const CHANNEL_SELECT = 8
export const LINK_STYLE = 5

/**
 * How long a click waits for the bot's answer before the member is told it did
 * not come. A bot has 15 minutes to answer a deferred click, but it is expected
 * to answer or acknowledge within 3 seconds (Discord's rule, which the SDKs
 * follow: reply, update or defer at once). 15 seconds covers a slow bot and a slow
 * link several times over, and is still short enough that a member who clicked
 * learns in the same moment that nothing came, and may click again. A bot that
 * deferred (callback 5 or 6) and answers later still shows its answer whenever it comes.
 */
export const ANSWER_WAIT_MS = 15_000

/**
 * How long a click the bot deferred (callback 5 or 6) waits for its answer:
 * the interaction's lifetime on the server, 15 minutes. The bot said it will
 * answer and may do so until then; after it the server refuses the answer, so
 * waiting longer would be waiting for nothing, and giving up sooner would call
 * a bot that keeps its word silent.
 */
export const DEFERRED_WAIT_MS = 15 * 60_000

const SELECTS: readonly unknown[] = [STRING_SELECT, USER_SELECT, ROLE_SELECT, CHANNEL_SELECT]
const BUTTON_STYLES: readonly unknown[] = [1, 2, 3, 4, LINK_STYLE]

const isObject = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value)

/**
 * Whether this app can show a component. A newer server or bot may send types
 * and button styles this app does not know (the enums are not trusted): those
 * are skipped, never guessed at.
 */
function isShowable(value: unknown): value is Component {
  if (!isObject(value)) return false
  if (value['type'] === BUTTON) return BUTTON_STYLES.includes(value['style'] ?? 2)
  return SELECTS.includes(value['type'])
}

export interface ReadRows { rows: Component[][]; skipped: boolean }

/** The rows of components this app can show, and whether anything was left out. */
export function actionRows(rows: unknown): ReadRows {
  let skipped = false
  const shown: Component[][] = []
  for (const row of Array.isArray(rows) ? rows : []) {
    const children: unknown[] = isObject(row) && row['type'] === 1 && Array.isArray(row['components']) ? row['components'] : []
    const known = children.filter(isShowable)
    if (known.length < children.length || children.length === 0) skipped = true
    if (known.length) shown.push(known)
  }
  return { rows: shown, skipped }
}

export function isSelect(c: Component): boolean {
  return SELECTS.includes(c.type)
}

/** The address a link button opens: http or https only, else ''. */
export function linkUrl(c: Component): string {
  if (c.style !== LINK_STYLE || !c.url) return ''
  try {
    const url = new URL(c.url)
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : ''
  } catch {
    return ''
  }
}

export function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

/** How many values a select takes (Discord's defaults: one). */
export function selectBounds(c: Component): { min: number; max: number } {
  const max = Math.max(1, Math.min(25, wholeNumber(c.max_values, 1)))
  const min = Math.max(0, Math.min(max, wholeNumber(c.min_values, 1)))
  return { min, max }
}

/** A whole number from the wire, or the fallback for anything else. */
export function wholeNumber(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isInteger(value) ? value : fallback
}

type SelectOption = NonNullable<Component['options']>[number]
export type ShownOption = SelectOption & { label: string; value: string }

/** The options of a string select that can be shown: each with a text label and value. */
export function stringOptions(c: Component): ShownOption[] {
  const options: unknown = c.options
  return (Array.isArray(options) ? options : []).filter((o): o is ShownOption =>
    isObject(o) && typeof o['label'] === 'string' && typeof o['value'] === 'string')
}

/** Whether a choice of count values may be sent. */
export function withinBounds(count: number, bounds: { min: number; max: number }): boolean {
  return count >= bounds.min && count <= bounds.max
}

/** The label a button shows: its label, else its emoji. */
export function buttonText(c: Component): string {
  return c.label || c.emoji?.name || ''
}

/** Where a use of a message's components goes. */
export function messageComponentsPath(channelId: string, messageId: string): string {
  return `/api/channels/${encodeURIComponent(channelId)}/messages/${encodeURIComponent(messageId)}/components`
}

/** Where a use of a private note's components goes. */
export function noteComponentsPath(channelId: string, noteId: string): string {
  return `/api/channels/${encodeURIComponent(channelId)}/ephemeral/${encodeURIComponent(noteId)}/components`
}

/** What a member reads when a click was refused. */
export function clickError(e: unknown, botName: string): string {
  if (isApiError(e)) {
    if (e.status === 409) return t('components.notConnected', { bot: botName })
    if (e.status === 404) return t('components.gone')
    if (e.status === 429) return t('components.slowDown')
  }
  return caughtErrorMessage(e, t('components.failed'))
}
