// Builders of modals and their text inputs.
import type { ActionRow, ModalData, TextInputComponent } from '../types/components.ts'
import { compact } from '../util.ts'
import { validateModal, validateTextInput } from './validate.ts'

export interface TextInputOptions {
  customId: string
  /** Up to 45 characters. */
  label: string
  /** Short (default) or Paragraph. */
  style?: 1 | 2
  minLength?: number
  maxLength?: number
  /** Default true. */
  required?: boolean
  /** Prefilled text. */
  value?: string
  placeholder?: string
}

/** A text input of a modal. */
export function textInput(o: TextInputOptions): TextInputComponent {
  const t = compact<TextInputComponent>({
    type: 4,
    custom_id: o.customId,
    label: o.label,
    style: o.style ?? 1,
    min_length: o.minLength,
    max_length: o.maxLength,
    required: o.required,
    value: o.value,
    placeholder: o.placeholder,
  })
  validateTextInput(t)
  return t
}

export interface ModalOptions {
  customId: string
  /** Up to 45 characters. */
  title: string
  /** 1 to 5 text inputs; each bare one gets its own row. */
  components: (TextInputComponent | ActionRow<TextInputComponent>)[]
}

/** A modal, the `data` of callback type 9 (see Interaction.showModal). */
export function modal(o: ModalOptions): ModalData {
  const rows = (o.components ?? []).map((c): ActionRow<TextInputComponent> =>
    c.type === 1 ? c : { type: 1, components: [c] },
  )
  const m: ModalData = { custom_id: o.customId, title: o.title, components: rows }
  validateModal(m)
  return m
}
