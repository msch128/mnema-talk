import type { VoiceUser } from '../../types/domain'
import type { StageHand, StageTalkPower, TalkReaction } from '../../types/stageEvents'
import type { VoiceState } from './state'

/** How long a live reaction stays over a tile. */
export const REACTION_MS = 3000
/** The most reactions kept at once; older ones go first (a busy Talk must not pile up). */
export const MAX_REACTIONS = 40

/** A live reaction in the Talk: shown briefly over the sender's tile, never kept. */
export interface LiveReaction {
  id: number
  channel_id: string
  user_id: string
  emoji: string
}

let reactionSeq = 0

function updateSeat(cs: VoiceState, channelId: string, userId: string, change: (user: VoiceUser) => VoiceUser) {
  const users = cs.channelUsers.value[channelId]
  const user = users?.[userId]
  if (users && user) users[userId] = change(user)
}

/** stage_hand: someone raised (at) or lowered their hand. */
export function handleStageHand(cs: VoiceState, event: StageHand) {
  const { channel_id, user_id, raised, at } = event || {}
  updateSeat(cs, channel_id, user_id, user => {
    const next = { ...user }
    if (raised) next.hand_raised_at = at ?? new Date().toISOString()
    else delete next.hand_raised_at
    return next
  })
}

/** stage_talk_power: someone may speak now, or may not any more. Accepted hands go down. */
export function handleStageTalkPower(cs: VoiceState, event: StageTalkPower) {
  const { channel_id, user_id, granted } = event || {}
  updateSeat(cs, channel_id, user_id, user => {
    const next = { ...user, talk_power: !!granted }
    if (granted) delete next.hand_raised_at
    return next
  })
}

/** talk_reaction: a member sent an emoji; it floats for REACTION_MS and is forgotten. */
export function handleTalkReaction(cs: VoiceState, event: TalkReaction) {
  const { channel_id, user_id, emoji } = event || {}
  if (!channel_id || !user_id || !emoji || cs.currentChannelId.value !== channel_id) return
  const item: LiveReaction = { id: ++reactionSeq, channel_id, user_id, emoji }
  cs.talkReactions.value = [...cs.talkReactions.value, item].slice(-MAX_REACTIONS)
  setTimeout(() => {
    cs.talkReactions.value = cs.talkReactions.value.filter(r => r.id !== item.id)
  }, REACTION_MS)
}

/** Whether the member may speak in the Talk's Stage as the server last said. */
export function hasTalkPower(cs: VoiceState, channelId: string | null, userId: string | null): boolean {
  if (!channelId || !userId) return false
  return !!cs.channelUsers.value[channelId]?.[userId]?.talk_power
}
