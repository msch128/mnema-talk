import { t } from '../../i18n'
import { openSettings, type SettingsField } from '../../lib/settingsDialog'
import { useToastStore, type ToastAction, type ToastOptions } from '../../stores/toast'

const shown = new Set<string>()

/** A toast button that opens the audio settings on the field that fixes the problem. */
export function audioSettingsAction(field: SettingsField): ToastAction {
  return { label: t('audio.openSettings'), onClick: () => openSettings('audio', { field }) }
}

/** Options that put the "Open audio settings" button on a toast; `rest` keeps a second button of its own. */
export function withAudioSettings(field: SettingsField, rest: Pick<ToastOptions, 'secondary'> = {}): ToastOptions {
  return { action: audioSettingsAction(field), ...rest }
}

/** Tells the user once per page load, so a browser that cannot do something does not say so on every join. */
export function noticeOnce(key: string, field?: SettingsField) {
  if (shown.has(key)) return
  shown.add(key)
  useToastStore().info(t(key), field ? withAudioSettings(field) : undefined)
}

/** For tests. */
export function resetNotices() {
  shown.clear()
}
