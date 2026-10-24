import type { Channel } from '../../types/domain'
import type { ServerEventPayloads } from '../../types/events'
import type { Cx } from './state'

/** talk_leader: the leader of a temporary Talk changed (null: nobody leads it). */
export function onTalkLeader({ cs }: Cx, p: ServerEventPayloads['talk_leader']) {
  if (!cs.allChannels.value.some(c => c.id === p.channel_id)) return
  const withLeader = (c: Channel): Channel => {
    if (c.id !== p.channel_id) return c
    const next = { ...c }
    if (p.leader_id) next.leader_id = p.leader_id
    else delete next.leader_id
    return next
  }
  cs.categories.value = cs.categories.value.map(cat => (
    cat.channels.some(c => c.id === p.channel_id) ? { ...cat, channels: cat.channels.map(withLeader) } : cat
  ))
  if (cs.uncategorized.value.some(c => c.id === p.channel_id)) cs.uncategorized.value = cs.uncategorized.value.map(withLeader)
}
