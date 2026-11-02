import { ref } from 'vue'
import { useAccountStore } from '../stores/account'
import { readItem, writeItem } from './voicePlayback'

/** The sections of the settings dialog, in the order they are listed: this device first, then the account. */
export const SETTINGS_TABS = ['audio', 'app', 'signin', 'sessions', 'privacy', 'roles', 'data'] as const
export type SettingsTab = typeof SETTINGS_TABS[number]

const LAST_TAB_KEY = 'mnema_settings_tab'

function lastTab(): SettingsTab {
  const stored = readItem(LAST_TAB_KEY)
  return SETTINGS_TABS.find(tab => tab === stored) ?? SETTINGS_TABS[0]
}

/** The fields other parts of the app point to (`data-settings-field` in the section's markup). */
export type SettingsField = 'input-device' | 'output-device' | 'noise' | 'push'

/** The section that is (or will be) shown; the dialog follows it and writes it back. */
export const settingsTab = ref<SettingsTab>(lastTab())

/** The field to scroll into view and focus once its section is on screen; the dialog clears it. */
export const settingsField = ref<SettingsField | null>(null)

export function isSettingsTab(value: string | undefined): value is SettingsTab {
  return (SETTINGS_TABS as readonly string[]).includes(value ?? '')
}

/** A section the member picked is where the dialog opens next time. */
export function rememberSettingsTab(tab: SettingsTab) {
  settingsTab.value = tab
  writeItem(LAST_TAB_KEY, tab)
}

/**
 * Opens the settings dialog on one section (the one visited last by default),
 * optionally on one field of it. App.vue shows the dialog while the account
 * store says it is open; the address follows (/settings/:tab). Asking while it
 * is open only moves it.
 */
export function openSettings(tab?: SettingsTab, opts: { field?: SettingsField } = {}) {
  settingsTab.value = tab ?? lastTab()
  settingsField.value = opts.field ?? null
  const account = useAccountStore()
  if (!account.dialogOpen) account.open()
}

const FOCUSABLE = 'input, select, textarea, button, [tabindex="0"]'

/** The control to focus for a field named by `data-settings-field` (its first control, for a group), or null when it is not rendered (yet). */
export function settingsFieldTarget(root: ParentNode, field: string): HTMLElement | null {
  const el = root.querySelector<HTMLElement>(`[data-settings-field="${field}"]`)
  if (!el) return null
  if (el.matches(FOCUSABLE)) return el
  return el.querySelector<HTMLElement>('[aria-checked="true"]') ?? el.querySelector<HTMLElement>(FOCUSABLE) ?? el
}

/** Scrolls the field into view and focuses it. False when it is not there (yet). */
export function focusSettingsField(root: ParentNode, field: string): boolean {
  const target = settingsFieldTarget(root, field)
  if (!target) return false
  target.scrollIntoView?.({ block: 'center' })
  target.focus({ preventScroll: true })
  return true
}
