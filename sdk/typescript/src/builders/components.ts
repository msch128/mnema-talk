// Builders of message components: plain JSON in Discord's shape, checked
// against the documented limits when built.
import type {
  ActionRow,
  ButtonComponent,
  ChannelSelectComponent,
  ComponentEmoji,
  MessageRowComponent,
  RoleSelectComponent,
  SelectOption,
  StringSelectComponent,
  TextInputComponent,
  UserSelectComponent,
} from '../types/components.ts'
import { compact } from '../util.ts'
import { fail, validateButton, validateMessageRow, validateSelect, validateTextInput } from './validate.ts'

type Emoji = string | ComponentEmoji

function emojiOf(e: Emoji | undefined): ComponentEmoji | undefined {
  if (e === undefined) return undefined
  return typeof e === 'string' ? { name: e } : e
}

/**
 * An action row: up to 5 buttons, or exactly one select (in a message), or
 * exactly one text input (in a modal).
 */
export function actionRow(...components: (MessageRowComponent | MessageRowComponent[])[]): ActionRow<MessageRowComponent>
export function actionRow(input: TextInputComponent): ActionRow<TextInputComponent>
export function actionRow(
  ...components: (MessageRowComponent | TextInputComponent | (MessageRowComponent | TextInputComponent)[])[]
): ActionRow<MessageRowComponent> | ActionRow<TextInputComponent> {
  const flat = components.flat()
  if (flat.some((c) => c.type === 4)) {
    if (flat.length !== 1) fail('a modal row holds exactly one text input')
    validateTextInput(flat[0] as TextInputComponent)
    return { type: 1, components: [flat[0] as TextInputComponent] }
  }
  const row = { type: 1 as const, components: flat as MessageRowComponent[] }
  validateMessageRow(row)
  return row
}

export interface ButtonOptions {
  customId: string
  label?: string
  emoji?: Emoji
  /** Primary (default), Secondary, Success or Danger; use linkButton for links. */
  style?: 1 | 2 | 3 | 4
  disabled?: boolean
}

/** A button that sends an interaction with its customId when clicked. */
export function button(o: ButtonOptions): ButtonComponent {
  if ((o.style as number | undefined) === 5) fail('use linkButton() for a link button')
  const b = compact<ButtonComponent>({
    type: 2,
    style: o.style ?? 1,
    label: o.label,
    emoji: emojiOf(o.emoji),
    custom_id: o.customId,
    disabled: o.disabled,
  })
  validateButton(b)
  return b
}

export interface LinkButtonOptions {
  url: string
  label?: string
  emoji?: Emoji
  disabled?: boolean
}

/** A button that opens a URL (style 5); clicks reach no bot. */
export function linkButton(o: LinkButtonOptions): ButtonComponent {
  const b = compact<ButtonComponent>({
    type: 2,
    style: 5,
    label: o.label,
    emoji: emojiOf(o.emoji),
    url: o.url,
    disabled: o.disabled,
  })
  validateButton(b)
  return b
}

export interface SelectOptions {
  customId: string
  placeholder?: string
  /** 0 to 25, default 1. */
  minValues?: number
  /** 1 to 25, default 1. */
  maxValues?: number
  disabled?: boolean
}

export interface StringSelectOptionInput {
  label: string
  value: string
  description?: string
  emoji?: Emoji
  default?: boolean
}

function selectBase(o: SelectOptions) {
  return {
    custom_id: o.customId,
    placeholder: o.placeholder,
    min_values: o.minValues,
    max_values: o.maxValues,
    disabled: o.disabled,
  }
}

/** A select menu of fixed options (1 to 25). */
export function stringSelect(o: SelectOptions & { options: StringSelectOptionInput[] }): StringSelectComponent {
  const options = (o.options ?? []).map((opt) =>
    compact<SelectOption>({ ...opt, emoji: emojiOf(opt.emoji) }),
  )
  const s = compact<StringSelectComponent>({ type: 3, ...selectBase(o), options })
  validateSelect(s)
  return s
}

/** A select menu of members. */
export function userSelect(o: SelectOptions): UserSelectComponent {
  const s = compact<UserSelectComponent>({ type: 5, ...selectBase(o) })
  validateSelect(s)
  return s
}

/** A select menu of roles (not @everyone, not the managed bot roles). */
export function roleSelect(o: SelectOptions): RoleSelectComponent {
  const s = compact<RoleSelectComponent>({ type: 6, ...selectBase(o) })
  validateSelect(s)
  return s
}

/** A select menu of text, forum and announcement channels. */
export function channelSelect(o: SelectOptions): ChannelSelectComponent {
  const s = compact<ChannelSelectComponent>({ type: 8, ...selectBase(o) })
  validateSelect(s)
  return s
}
