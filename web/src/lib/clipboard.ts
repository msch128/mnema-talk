// Copying text for the member, with the outcome told in a toast.
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

/** Copies text; on failure (denied permission, insecure origin) the toast carries the text so it can be copied by hand. */
export async function copyToClipboard(text: string, okMessage: string): Promise<boolean> {
  const toasts = useToastStore()
  try {
    await navigator.clipboard.writeText(text)
    toasts.success(okMessage)
    return true
  } catch {
    toasts.error(t('admin.copyFailed') + ' ' + text.slice(0, 200))
    return false
  }
}
