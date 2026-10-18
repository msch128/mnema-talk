import type { Channel, Category } from '../types/domain'
import { caughtErrorMessage } from '../lib/api'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import { t } from '../i18n'

/** Deleting and duplicating channels and categories from the sidebar, with their confirmations and toasts. */
export function useChannelAdminActions(revealChannel: (id: string) => void) {
  const chatStore = useChatStore()
  const toasts = useToastStore()

  async function deleteChannel(channel: Channel) {
    const isVoice = channel.type === 'voice'
    const ok = await confirm({
      title: t(isVoice ? 'sidebar.deleteVoiceChannelTitle' : 'sidebar.deleteChannelTitle', { name: channel.name }),
      body: t(isVoice ? 'sidebar.deleteVoiceChannelBody' : 'sidebar.deleteChannelBody'),
      confirmLabel: t('common.delete'),
      danger: true
    })
    if (!ok) return
    try {
      await chatStore.deleteChannel(channel.id)
      toasts.success(t('sidebar.channelDeleted'))
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('sidebar.deleteFailed')))
    }
  }

  async function deleteCategory(category: Category) {
    const ok = await confirm({
      title: t('sidebar.deleteCategoryTitle', { name: category.name }),
      body: t('sidebar.deleteCategoryBody'),
      confirmLabel: t('common.delete'),
      danger: true
    })
    if (!ok) return
    try {
      await chatStore.deleteCategory(category.id)
      toasts.success(t('sidebar.categoryDeleted'))
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('sidebar.deleteFailed')))
    }
  }

  async function duplicateChannel(channel: Channel) {
    try {
      const created = await chatStore.duplicateChannel(channel.id)
      toasts.success(t('sidebar.channelDuplicated', { name: channel.name }))
      // The copy sits right below the original; show it without navigating.
      if (created?.id) revealChannel(created.id)
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('sidebar.duplicateFailed')))
    }
  }

  return { deleteChannel, deleteCategory, duplicateChannel }
}
