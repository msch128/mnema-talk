// Early checks of the documented component and modal limits, on the plain
// JSON a builder returns or a bot writes by hand (doc/bots.md).
import type {
  ActionRow,
  ButtonComponent,
  MessageRowComponent,
  ModalData,
  SelectComponent,
  TextInputComponent,
} from '../types/components.ts'

/** A component, modal or command that breaks a documented limit. */
export class ValidationError extends Error {
  override name = 'ValidationError'
}

export function fail(message: string): never {
  throw new ValidationError(message)
}

export function checkLength(what: string, value: unknown, min: number, max: number): void {
  if (typeof value !== 'string') fail(`${what} must be a string`)
  const n = [...value].length
  if (n < min || n > max) fail(`${what} must be ${min} to ${max} characters, got ${n}`)
}

function checkOptionalLength(what: string, value: unknown, max: number): void {
  if (value !== undefined) checkLength(what, value, 0, max)
}

export function checkInt(what: string, value: unknown, min: number, max: number): void {
  if (value === undefined) return
  if (typeof value !== 'number' || !Number.isInteger(value) || value < min || value > max) {
    fail(`${what} must be a whole number from ${min} to ${max}, got ${String(value)}`)
  }
}

function checkCustomId(what: string, id: unknown): void {
  checkLength(`${what} custom_id`, id, 1, 100)
}

function checkUrl(url: unknown): void {
  checkLength('link button url', url, 1, 512)
  if (!/^https?:\/\//i.test(url as string)) fail('link button url must start with http:// or https://')
}

export function validateButton(b: ButtonComponent): void {
  checkInt('button style', b.style, 1, 5)
  if (b.label === undefined && b.emoji === undefined) fail('a button needs a label or an emoji')
  checkOptionalLength('button label', b.label, 80)
  if (b.emoji !== undefined && (typeof b.emoji.name !== 'string' || b.emoji.name === '')) {
    fail('button emoji needs a name (a Unicode emoji)')
  }
  if (b.style === 5) {
    if (b.custom_id !== undefined) fail('a link button has a url, not a custom_id')
    checkUrl(b.url)
    return
  }
  if (b.url !== undefined) fail('only a link button (style 5) has a url')
  checkCustomId('button', b.custom_id)
}

function checkMinMax(what: string, min: number | undefined, max: number | undefined): void {
  checkInt(`${what} min_values`, min, 0, 25)
  checkInt(`${what} max_values`, max, 1, 25)
  if ((min ?? 1) > (max ?? 1)) fail(`${what} min_values must not exceed max_values`)
}

export function validateSelect(s: SelectComponent): void {
  checkCustomId('select', s.custom_id)
  checkOptionalLength('select placeholder', s.placeholder, 150)
  checkMinMax('select', s.min_values, s.max_values)
  if (s.type !== 3) return
  if (!Array.isArray(s.options) || s.options.length < 1 || s.options.length > 25) {
    fail('a string select needs 1 to 25 options')
  }
  const values = new Set<string>()
  for (const o of s.options) {
    checkLength('select option label', o.label, 1, 100)
    checkLength('select option value', o.value, 1, 100)
    checkOptionalLength('select option description', o.description, 100)
    if (values.has(o.value)) fail(`select option values must be unique: "${o.value}" repeats`)
    values.add(o.value)
  }
}

export function validateTextInput(t: TextInputComponent): void {
  checkCustomId('text input', t.custom_id)
  checkLength('text input label', t.label, 1, 45)
  checkInt('text input style', t.style, 1, 2)
  checkInt('text input min_length', t.min_length, 0, 4000)
  checkInt('text input max_length', t.max_length, 1, 4000)
  if (t.min_length !== undefined && t.max_length !== undefined && t.min_length > t.max_length) {
    fail('text input min_length must not exceed max_length')
  }
  checkOptionalLength('text input value', t.value, 4000)
  checkOptionalLength('text input placeholder', t.placeholder, 100)
}

const selectTypes = new Set([3, 5, 6, 8])

function validateRowChild(c: MessageRowComponent): void {
  if (c.type === 2) validateButton(c)
  else if (selectTypes.has(c.type)) validateSelect(c)
  else fail(`type ${String((c as { type: unknown }).type)} cannot be in a message's action row`)
}

/** An action row of a message: 1 to 5 buttons, or exactly one select. */
export function validateMessageRow(row: ActionRow<MessageRowComponent>): void {
  if (row?.type !== 1 || !Array.isArray(row.components)) fail('a message row must be an action row (type 1)')
  const n = row.components.length
  if (n === 0) fail('an action row needs at least one component')
  const selects = row.components.filter((c) => selectTypes.has(c.type)).length
  if (selects > 0 && n > 1) fail('an action row holds exactly one select, or up to 5 buttons')
  if (n > 5) fail(`an action row holds at most 5 buttons, got ${n}`)
  row.components.forEach(validateRowChild)
}

/** The components of a message: up to 5 rows, 25 components, unique custom_ids. */
export function validateMessageComponents(rows: ActionRow<MessageRowComponent>[]): void {
  if (!Array.isArray(rows)) fail('components must be an array of action rows')
  if (rows.length > 5) fail(`a message has at most 5 action rows, got ${rows.length}`)
  rows.forEach(validateMessageRow)
  const ids = rows.flatMap((r) => r.components.map((c) => c.custom_id)).filter((id) => id !== undefined)
  checkUnique('custom_id', ids, 'a message')
}

function checkUnique(what: string, ids: string[], where: string): void {
  const seen = new Set<string>()
  for (const id of ids) {
    if (seen.has(id)) fail(`${what} "${id}" is used twice in ${where}`)
    seen.add(id)
  }
}

/** A modal: a title of up to 45 characters and 1 to 5 rows of one text input each. */
export function validateModal(m: ModalData): void {
  checkCustomId('modal', m.custom_id)
  checkLength('modal title', m.title, 1, 45)
  if (!Array.isArray(m.components) || m.components.length < 1 || m.components.length > 5) {
    fail('a modal has 1 to 5 rows')
  }
  for (const row of m.components) {
    if (row?.type !== 1 || row.components?.length !== 1 || row.components[0]?.type !== 4) {
      fail('each modal row is an action row with exactly one text input')
    }
    validateTextInput(row.components[0])
  }
  checkUnique('custom_id', m.components.map((r) => r.components[0]!.custom_id), 'a modal')
}
