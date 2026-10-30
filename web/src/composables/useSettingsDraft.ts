// The one pattern for settings that are saved with an explicit Save button
// (doc/settings-ux.md): the form edits a draft, nothing is persisted until Save,
// "changed" is decided field by field on normalised values, and leaving with
// unsaved changes asks first. Shared configuration (channels, roles, community,
// Talks) uses this; personal preferences save at once and do not.
import { computed, reactive, ref, watch } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { confirmDiscard, saveKeys, useLeaveGuard } from './draftGuard'
import { t } from '../i18n'

/** Per-field normalisers: values that mean the same thing compare as equal. A normaliser also sees the whole state, for fields that only matter next to another one. */
export type Normalisers<T> = { [K in keyof T]?: (value: T[K], state: T) => unknown }

/** Normalisers for the usual field shapes. */
export const normalise = {
  /** Surrounding whitespace never counts; null and undefined are empty. */
  text: (value: string | null | undefined): string => (value ?? '').trim(),
  /** A whole count where empty, 0 and not-a-number all mean "none / unlimited". */
  count: (value: number | string | null | undefined): number => {
    const n = typeof value === 'string' ? Number(value.trim()) : value
    return typeof n === 'number' && Number.isFinite(n) ? Math.max(0, Math.trunc(n)) : 0
  },
  /** A list where order and duplicates do not matter. */
  set: (value: readonly string[] | null | undefined): string[] => [...new Set(value ?? [])].sort(),
  /** Anything unset counts as off. */
  flag: (value: boolean | null | undefined): boolean => value === true
}

export { confirmDiscard, provideDraftScope } from './draftGuard'

export type FieldErrors<T> = Partial<Record<keyof T, string>>

export interface SaveContext<T> {
  /** Only the fields that differ from the saved state. */
  changes: Partial<T>
  changed: (keyof T)[]
  draft: T
  /**
   * Marks fields as saved as soon as they are, so a later failure leaves only the rest changed.
   * What was sent counts as saved: an edit made while the save ran stays unsaved.
   * `values` overrides what the server stored instead (a one-off box it cleared).
   */
  commit: (keys: (keyof T)[], values?: Partial<T>) => void
}

export interface SettingsDraftOptions<T extends object> {
  /** The saved state; read again whenever it changes. */
  source: () => T
  normalisers?: Normalisers<T>
  /** Field errors of the draft; Save stays off while there are any. */
  validate?: (draft: T) => FieldErrors<T>
  /** Writes the changes. Throw to report a failure; call `commit` for parts that already went through. */
  save: (context: SaveContext<T>) => Promise<void>
  /** Runs after a successful save (a toast, a reload). */
  saved?: () => void | Promise<void>
  /** Save stays off while this returns true (still loading, not allowed). */
  blocked?: () => boolean
}

const isMac = typeof navigator !== 'undefined' && /mac|iphone|ipad/i.test(navigator.platform)
/** The save shortcut as shown to people. */
export const saveShortcutLabel = isMac ? '⌘S' : 'Ctrl+S'

const copy = <V>(value: V): V => (Array.isArray(value) ? ([...value] as V) : value)

export function useSettingsDraft<T extends object>(options: SettingsDraftOptions<T>) {
  const keyOf = <K extends keyof T>(k: K, state: T) => JSON.stringify(options.normalisers?.[k] ? options.normalisers[k](state[k], state) : state[k]) ?? ''
  const keysOf = (o: T) => Object.keys(o) as (keyof T)[]

  const initial = options.source()
  const fields = keysOf(initial)
  const saved = reactive({ ...initial }) as T
  const draft = reactive({ ...initial }) as T
  for (const k of fields) { saved[k] = copy(initial[k]); draft[k] = copy(initial[k]) }
  const savedKeys = reactive<Record<string, string>>({})
  for (const k of fields) savedKeys[k as string] = keyOf(k, saved)

  const isChanged = (k: keyof T) => keyOf(k, draft) !== savedKeys[k as string]
  const changed = computed(() => fields.filter(isChanged))
  const dirty = computed(() => changed.value.length > 0)
  const errors = computed<FieldErrors<T>>(() => options.validate?.(draft) ?? {})
  const valid = computed(() => Object.keys(errors.value).length === 0)
  const saving = ref(false)
  const error = ref('')
  const canSave = computed(() => dirty.value && valid.value && !saving.value && !options.blocked?.())

  const refreshSavedKeys = () => { for (const k of fields) savedKeys[k as string] = keyOf(k, saved) }

  // The saved state moved (another admin, a reload after our own save): fields the
  // member has not touched follow it, fields they are editing keep their value.
  watch(options.source, next => {
    const untouched = fields.filter(k => !isChanged(k))
    for (const k of fields) saved[k] = copy(next[k])
    refreshSavedKeys()
    for (const k of untouched) draft[k] = copy(next[k])
  }, { deep: true })

  function commit(keys: (keyof T)[], sent: Partial<T>, values: Partial<T> = {}) {
    for (const k of keys) saved[k] = copy(k in values ? values[k] : sent[k]) as T[keyof T]
    refreshSavedKeys()
  }

  async function save(): Promise<boolean> {
    if (!canSave.value) return false
    const keys = [...changed.value]
    const changes: Partial<T> = {}
    for (const k of keys) changes[k] = copy(draft[k])
    saving.value = true
    error.value = ''
    let wrote = false
    const done = new Set<keyof T>()
    const track = (ks: (keyof T)[], values?: Partial<T>) => { wrote = wrote || ks.length > 0; ks.forEach(k => done.add(k)); commit(ks, changes, values) }
    try {
      await options.save({ changes, changed: keys, draft, commit: track })
      commit(keys.filter(k => !done.has(k)), changes)
    } catch (e) {
      const message = caughtErrorMessage(e, t('draft.saveFailed'))
      error.value = wrote ? t('draft.partialFailed', { message }) : message
      return false
    } finally {
      saving.value = false
    }
    await options.saved?.()
    return true
  }

  /** Throws away the edits and shows the saved state again. */
  function reset() {
    for (const k of changed.value) draft[k] = copy(saved[k])
    error.value = ''
  }

  const onKeydown = saveKeys(save)

  /** True when nothing is lost by leaving: the draft is clean or the member agreed. */
  async function guard(): Promise<boolean> {
    return !dirty.value || confirmDiscard()
  }

  useLeaveGuard(dirty)

  return { draft, dirty, changed, errors, valid, saving, error, canSave, save, reset, guard, onKeydown }
}
