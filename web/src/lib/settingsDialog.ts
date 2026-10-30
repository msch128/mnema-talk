import { ref } from 'vue'
import { useAccountStore } from '../stores/account'
import { readItem, writeItem } from './voicePlayback'

/** The sections of the settings dialog, in the order they are listed: this device first, then the account. */
export const SETTINGS_TABS = ['audio', 'app', 'signin', 'sessions', 'privacy', 'data'] as const
export type SettingsTab = typeof SETTINGS_TABS[number]

const LAST_TAB_KEY = 'mnema_settings_tab'

function lastTab(): SettingsTab {
  const stored = readItem(LAST_TAB_KEY)
  return SETTINGS_TABS.find(tab => tab === stored) ?? SETTINGS_TABS[0]
}

/** The section that is (or will be) shown; the dialog follows it and writes it back. */
export const settingsTab = ref<SettingsTab>(lastTab())

/** A section the member picked is where the dialog opens next time. */
export function rememberSettingsTab(tab: SettingsTab) {
  settingsTab.value = tab
  writeItem(LAST_TAB_KEY, tab)
}

/** Opens the settings dialog on one section, or on the one visited last. App.vue shows it while the account store says it is open. */
export function openSettings(tab?: SettingsTab) {
  settingsTab.value = tab ?? lastTab()
  useAccountStore().open()
}
