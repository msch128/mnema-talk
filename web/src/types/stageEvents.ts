// Stage and live-reaction events of a Talk (internal/ws/stage.go, reactions.go).
// All three go to the members of that Talk only and are never stored.
import { hasOwn, isIdentifier, isString, isTimestamp } from './validation'

/** A member raised (at set) or lowered their hand in the Stage the receiver is in. */
export interface StageHand { channel_id: string; user_id: string; raised: boolean; at?: string }
/** Whether a member may speak in a Stage now (given, or implied by Manage stage). */
export interface StageTalkPower { channel_id: string; user_id: string; granted: boolean }
/** A live reaction in a Talk: shown briefly, never stored. */
export interface TalkReaction { channel_id: string; user_id: string; emoji: string }

function inRoom(payload: Record<string, unknown>): boolean {
  return isIdentifier(payload['channel_id']) && isIdentifier(payload['user_id'])
}

/** Runtime checks of the three events, by event name. */
export const STAGE_EVENT_CHECKS: [string, (payload: Record<string, unknown>) => boolean][] = [
  ['stage_hand', p => inRoom(p) && typeof p['raised'] === 'boolean' && (!hasOwn(p, 'at') || isTimestamp(p['at']))],
  ['stage_talk_power', p => inRoom(p) && typeof p['granted'] === 'boolean'],
  ['talk_reaction', p => inRoom(p) && isString(p['emoji'], 128) && p['emoji'] !== '']
]
