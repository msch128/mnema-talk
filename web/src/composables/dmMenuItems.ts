// The context-menu entries that concern the relation to another member: write
// to them, block them, report one of their messages. Built from the stores
// like the other menu builders (useNavMenus, useMessageMenu); each list is
// empty on a server that lacks the feature.
import type { Message } from '../types/domain'
import type { ContextMenuItem } from '../components/menuTypes'
import { Eye, EyeOff, Flag, MessageCircle } from '@lucide/vue'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { openReport } from '../lib/reportDialog'
import { shownName } from '../lib/deletedUser'
import { t } from '../i18n'
import { useDmActions } from './useDmActions'

interface Person {
  id: string
  username?: string | undefined
  display_name?: string | undefined
}

function blockItem(person: Person): ContextMenuItem {
  const blocked = useBlocksStore().isBlocked(person.id)
  return {
    id: blocked ? 'unblock' : 'block',
    label: t(blocked ? 'blocks.unblock' : 'blocks.block'),
    icon: blocked ? Eye : EyeOff,
    action: () => { void useDmActions().toggleBlock(person) }
  }
}

/** "Send message" and "Block" for a member of the list or a profile. */
export function memberRelationItems(person: Person): ContextMenuItem[] {
  const actions = useDmActions()
  const items: ContextMenuItem[] = []
  if (actions.canMessage(person.id)) {
    items.push({ id: 'message', label: t('dm.message'), icon: MessageCircle, action: () => { void actions.messageMember(person.id) } })
  }
  if (actions.canBlock(person.id)) items.push(blockItem(person))
  return items
}

/** "Report message" and "Block" for the message of somebody else. */
export function messageSafetyItems(msg: Message): ContextMenuItem[] {
  if (!msg.user_id || msg.user_id === useAuthStore().user?.id) return []
  const items: ContextMenuItem[] = []
  if (useServerFeaturesStore().reports) {
    items.push({
      id: 'report',
      label: t('reports.menu'),
      icon: Flag,
      danger: true,
      action: () => openReport({ messageId: msg.id, author: shownName(msg) })
    })
  }
  if (useDmActions().canBlock(msg.user_id) && !msg.is_bot) items.push(blockItem({ id: msg.user_id, username: msg.username, display_name: msg.display_name }))
  return items
}
