// The forms bots open (doc/bots.md, Modals): one to five rows of one text
// input each. The server checks lengths and required inputs again on submit.
import { TEXT_INPUT, wholeNumber, type ActionRow } from './components'

export interface TextInput {
  customId: string
  label: string
  paragraph: boolean
  placeholder: string
  value: string
  min: number
  max: number
  required: boolean
}

const MAX_LENGTH = 4000

const text = (value: unknown): string => (typeof value === 'string' ? value : '')
const isObject = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value)

/**
 * The text inputs of a modal, in their rows' order. Anything else (a component
 * type this app does not know) is left out, like in a message.
 */
export function textInputs(rows: readonly ActionRow[]): TextInput[] {
  const children = (Array.isArray(rows) ? rows : []).flatMap((row: unknown) => (isObject(row) && Array.isArray(row['components']) ? row['components'] as unknown[] : []))
  return children.filter(isObject).filter(c => c['type'] === TEXT_INPUT && text(c['custom_id']) !== '').map(c => {
    const max = Math.max(1, Math.min(MAX_LENGTH, wholeNumber(c['max_length'], MAX_LENGTH)))
    return {
      customId: text(c['custom_id']),
      label: text(c['label']),
      paragraph: c['style'] === 2,
      placeholder: text(c['placeholder']),
      value: text(c['value']).slice(0, max),
      min: Math.max(0, Math.min(max, wholeNumber(c['min_length'], 0))),
      max,
      required: c['required'] !== false
    }
  })
}

export type InputProblem = 'required' | 'short' | null

/** Why a value cannot be sent yet, or null. An optional input may stay empty. */
export function inputProblem(input: TextInput, value: string): InputProblem {
  const length = [...value].length
  if (length === 0) return input.required ? 'required' : null
  return length < input.min ? 'short' : null
}

/** The length a counter shows: code points, as the server counts them. */
export function lengthOf(value: string): number {
  return [...value].length
}

export function modalPath(channelId: string, modalId: string): string {
  return `/api/channels/${encodeURIComponent(channelId)}/modals/${encodeURIComponent(modalId)}`
}
