// What a member does to a conversation of their list: favourite it, close it,
// mute it, and the context-menu entries that offer the same. Failures are
// said in a toast; the store has already put the list back.
import type { ChatDirectMessage } from '../types/rest'
import type { ContextMenuItem } from '../components/menuTypes'
import { ArrowDown, ArrowUp, Bell, BellOff, CheckCheck, Star, StarOff, X } from '@lucide/vue'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

export function useDmListActions() {
  const chat = useChatStore()
  const toasts = useToastStore()

  async function guarded(work: Promise<unknown>) {
    try {
      await work
    } catch (error) {
      console.warn('Failed to change the conversation list:', error)
      toasts.error(t('dm.prefsFailed'))
    }
  }

  const toggleFavourite = (dm: ChatDirectMessage) => guarded(chat.toggleFavourite(dm.id))
  const hide = (dm: ChatDirectMessage) => guarded(chat.setDmHidden(dm.id, true))
  const move = (dm: ChatDirectMessage, step: -1 | 1) => guarded(chat.moveFavourite(dm.id, step))
  const reorder = (ids: string[]) => guarded(chat.setFavourites(ids))

  /** Muting is the conversation's own setting; unmuting follows "mute all" when that is on. */
  function toggleMute(dm: ChatDirectMessage) {
    const muted = chat.notificationLevel(dm.id) === 'mute'
    const level = muted ? (chat.dmSettings.mute_all ? 'all' : 'inherit') : 'mute'
    return guarded(chat.setNotificationLevel(dm.id, level))
  }

  /** The entries of a conversation's context menu; position is its place among the favourites. */
  function menuItems(dm: ChatDirectMessage, unread: number, position: { first: boolean; last: boolean } | null): ContextMenuItem[] {
    const favourite = position !== null
    const muted = chat.notificationLevel(dm.id) === 'mute'
    const items: ContextMenuItem[] = []
    if (unread > 0) items.push({ id: 'read', label: t('dm.markRead'), icon: CheckCheck, action: () => { void chat.markChannelRead(dm.id) } })
    items.push({
      id: 'favourite',
      label: t(favourite ? 'dm.unfavourite' : 'dm.favourite'),
      icon: favourite ? StarOff : Star,
      action: () => { void toggleFavourite(dm) }
    })
    if (position && !position.first) items.push({ id: 'up', label: t('dm.moveUp'), icon: ArrowUp, action: () => { void move(dm, -1) } })
    if (position && !position.last) items.push({ id: 'down', label: t('dm.moveDown'), icon: ArrowDown, action: () => { void move(dm, 1) } })
    items.push({ id: 'mute', label: t(muted ? 'dm.unmute' : 'dm.mute'), icon: muted ? Bell : BellOff, action: () => { void toggleMute(dm) } })
    if (!favourite) items.push({ type: 'separator' }, { id: 'close', label: t('dm.close'), icon: X, action: () => { void hide(dm) } })
    return items
  }

  return { toggleFavourite, hide, move, reorder, toggleMute, menuItems }
}
