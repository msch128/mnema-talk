import type { Ref } from 'vue'
import { pastedFiles, pickAttachable } from '../lib/composerFiles'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

/**
 * Pasted and dropped files take the same road as a file picked with the
 * button: they are put into the composer's file input, which fires its change
 * event, so the chat's own upload (size limit, permission, errors) runs.
 */
export function useComposerFiles(opts: { fileInput: Ref<HTMLInputElement | null>; canAttach: () => boolean; busy: () => boolean }) {
  const toasts = useToastStore()

  function attachFiles(files: readonly File[]) {
    if (!files.length) return
    if (!opts.canAttach()) {
      toasts.error(t('chat.noAttach'))
      return
    }
    if (opts.busy()) return
    const { file, extra, rejected } = pickAttachable(files)
    const input = opts.fileInput.value
    if (!file || !input) {
      toasts.error(t('chat.attachMediaOnly'))
      return
    }
    if (rejected && extra) toasts.info(t('chat.attachOneSkipped'))
    else if (rejected) toasts.info(t('chat.attachSkipped'))
    else if (extra) toasts.info(t('chat.attachFirstOnly'))
    const transfer = new DataTransfer()
    transfer.items.add(file)
    input.files = transfer.files
    input.dispatchEvent(new Event('change'))
  }

  /** Files on the clipboard attach; text pastes as it always did. */
  function onPaste(e: ClipboardEvent) {
    const files = pastedFiles(e.clipboardData)
    if (!files.length) return
    e.preventDefault()
    attachFiles(files)
  }

  return { attachFiles, onPaste }
}
