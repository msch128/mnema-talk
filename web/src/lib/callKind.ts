import type { Message } from '../types/domain'

/** "invite": the card of a call; "missed": the record of one nobody answered; null for any other message. */
export function callKindOf(msg: Pick<Message, 'embeds'>): 'invite' | 'missed' | null {
  for (const embed of msg.embeds ?? []) {
    if (embed.call === 'missed') return 'missed'
    if (embed.talk_id) return 'invite'
  }
  return null
}
