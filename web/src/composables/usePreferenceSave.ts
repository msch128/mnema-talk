// Personal preferences save at once (doc/settings-ux.md): the change applies
// the moment it is made and a quiet "Saved" confirms it next to the setting.
// A failure is reported with a toast and the caller puts the control back.
import { onBeforeUnmount, ref } from 'vue'
import { caughtErrorMessage } from '../lib/api'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

export function usePreferenceSave(visibleMs = 2000) {
  const toasts = useToastStore()
  const saved = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined

  function flash() {
    saved.value = true
    clearTimeout(timer)
    timer = setTimeout(() => { saved.value = false }, visibleMs)
  }
  onBeforeUnmount(() => clearTimeout(timer))

  /** Runs the change; shows "Saved" when it went through, a toast when it did not. Resolves to whether it was saved. */
  async function run(change: () => unknown, failure = t('draft.saveFailed')): Promise<boolean> {
    try {
      await change()
    } catch (e) {
      toasts.error(caughtErrorMessage(e, failure))
      return false
    }
    flash()
    return true
  }

  return { saved, flash, run }
}
