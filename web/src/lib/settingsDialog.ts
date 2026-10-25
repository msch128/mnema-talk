import { ref } from 'vue'
import { useAccountStore } from '../stores/account'

/** The sections of the settings dialog, in the order they are listed. */
export const SETTINGS_TABS = ['signin', 'sessions', 'privacy', 'data', 'app', 'audio'] as const
export type SettingsTab = typeof SETTINGS_TABS[number]

/** The section that is (or will be) shown; the dialog follows it and writes it back. */
export const settingsTab = ref<SettingsTab>('signin')

/** Opens the settings dialog on one section. App.vue shows it while the account store says it is open. */
export function openSettings(tab: SettingsTab = 'signin') {
  settingsTab.value = tab
  useAccountStore().open()
}
