// Message components and modals in Discord's wire shape (doc/bots.md,
// "Message components" and "Modals").

export interface ComponentEmoji {
  /** A Unicode emoji. */
  name: string
}

export interface ButtonComponent {
  type: 2
  /** 1 primary, 2 secondary, 3 success, 4 danger, 5 link. */
  style: 1 | 2 | 3 | 4 | 5
  label?: string
  emoji?: ComponentEmoji
  /** Set on every style but link. */
  custom_id?: string
  /** Set on a link button only (http or https). */
  url?: string
  disabled?: boolean
}

export interface SelectOption {
  label: string
  value: string
  description?: string
  emoji?: ComponentEmoji
  default?: boolean
}

interface SelectBase {
  custom_id: string
  placeholder?: string
  min_values?: number
  max_values?: number
  disabled?: boolean
}

export interface StringSelectComponent extends SelectBase {
  type: 3
  options: SelectOption[]
}

export interface UserSelectComponent extends SelectBase {
  type: 5
}

export interface RoleSelectComponent extends SelectBase {
  type: 6
}

export interface ChannelSelectComponent extends SelectBase {
  type: 8
}

export type SelectComponent = StringSelectComponent | UserSelectComponent | RoleSelectComponent | ChannelSelectComponent

export interface TextInputComponent {
  type: 4
  custom_id: string
  label?: string
  /** 1 short, 2 paragraph. */
  style?: 1 | 2
  min_length?: number
  max_length?: number
  required?: boolean
  value?: string
  placeholder?: string
}

/** A component inside an action row of a message. */
export type MessageRowComponent = ButtonComponent | SelectComponent

export interface ActionRow<C = MessageRowComponent> {
  type: 1
  components: C[]
}

/** The rows of a message: up to 5. */
export type MessageComponents = ActionRow<MessageRowComponent>[]

/** Any component as it can appear on the wire. */
export type Component = ActionRow<MessageRowComponent | TextInputComponent> | MessageRowComponent | TextInputComponent

export interface ModalData {
  custom_id: string
  title: string
  /** 1 to 5 rows of one text input each. */
  components: ActionRow<TextInputComponent>[]
}

/** A submitted text input inside a modal submit interaction. */
export interface SubmittedTextInput {
  type: 4
  custom_id: string
  value: string
}
