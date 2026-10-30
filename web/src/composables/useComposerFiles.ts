import { pastedFiles } from '../lib/composerFiles'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

/**
 * Pasted, dropped and picked files are staged in the composer's attachment
 * tray, never sent on their own: the member adds text and sends the message.
 */
export function useComposerFiles(opts: { add: (files: readonly File[]) => void; canAttach: () => boolean; busy: () => boolean }) {
  const toasts = useToastStore()

  function attachFiles(files: readonly File[]) {
    if (!files.length) return
    if (!opts.canAttach()) {
      toasts.error(t('chat.noAttach'))
      return
    }
    if (opts.busy()) return
    opts.add(files)
  }

  /** Files on the clipboard are staged; text pastes as it always did. */
  function onPaste(e: ClipboardEvent) {
    const files = pastedFiles(e.clipboardData)
    if (!files.length) return
    e.preventDefault()
    attachFiles(files)
  }

  /** The hidden file input: stages what was picked and clears it, so the same file can be picked again. */
  function onPick(e: Event) {
    const input = e.target as HTMLInputElement
    attachFiles(Array.from(input.files ?? []))
    input.value = ''
  }

  return { attachFiles, onPaste, onPick }
}
