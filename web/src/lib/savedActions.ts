// Saving and unsaving a message from anywhere (menu, key, message row): the
// toast says where a saved message went and offers to show it.
import type { Message } from '../types/domain'
import { useSavedStore } from '../stores/saved'
import { useToastStore } from '../stores/toast'
import { caughtErrorMessage } from './api'
import { openSavedPanel } from './savedPanel'
import { t } from '../i18n'

const VIEW_TOAST_MS = 8000

async function undoRemove(messageId: string, note: string) {
  try {
    await useSavedStore().save(messageId, note)
  } catch (err) {
    useToastStore().error(caughtErrorMessage(err, t('saved.saveFailed')))
  }
}

/** Adds the message to the member's private saved list, or takes it off. Only on servers that have the feature. */
export async function toggleSavedMessage(msg: Pick<Message, 'id'>): Promise<void> {
  const saved = useSavedStore()
  const toasts = useToastStore()
  const was = saved.isSaved(msg.id)
  try {
    if (was) {
      const note = saved.items.find(e => e.message_id === msg.id)?.note ?? ''
      await saved.remove(msg.id)
      // The private note goes with the save: Undo puts both back.
      toasts.success(t('saved.removed'), {
        duration: VIEW_TOAST_MS,
        action: { label: t('saved.undo'), onClick: () => undoRemove(msg.id, note) }
      })
    } else {
      await saved.save(msg.id)
      saved.hintPending = true
      toasts.success(t('saved.added'), {
        duration: VIEW_TOAST_MS,
        action: { label: t('saved.view'), onClick: () => openSavedPanel({ tab: 'saved', messageId: msg.id }) }
      })
    }
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t(was ? 'saved.removeFailed' : 'saved.saveFailed')))
  }
}
