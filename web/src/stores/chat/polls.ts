import type { Poll, PollCreateRequest, PollDetail } from '../../types/domain'
import { decodeMessage, decodePollDetail } from '../../types/domain'
import { api } from '../../lib/api'
import { encodeShortcodes } from '../../lib/customEmoji'
import { useEmojiStore } from '../emoji'
import { assertCommunity, type Cx } from './state'
import { applyToMessage, insertMessage } from './messageWindow'

/** The poll of a message as the server last told it (no one's own votes). */
export function applyPoll(cx: Cx, messageId: string, poll: Poll) {
  applyToMessage(cx, messageId, m => { m.poll = poll })
}

function tallies({ my_votes, has_voted, ...poll }: PollDetail): Poll {
  void my_votes
  void has_voted
  return poll
}

function pollPath(channelId: string, messageId: string) {
  return `/api/channels/${channelId}/messages/${messageId}/poll`
}

/** The poll with the caller's own votes and whether they voted (an anonymous poll knows only the latter). */
export async function fetchPoll(cx: Cx, channelId: string, messageId: string): Promise<PollDetail> {
  const community = cx.cs.communityGeneration
  const detail = await api(pollPath(channelId, messageId), { decode: decodePollDetail })
  assertCommunity(cx, community)
  applyPoll(cx, messageId, tallies(detail))
  return detail
}

/** Replaces the caller's votes (an empty list withdraws them); the one vote of an anonymous poll is final. */
export async function votePoll(cx: Cx, channelId: string, messageId: string, optionIds: string[]): Promise<PollDetail> {
  const community = cx.cs.communityGeneration
  const detail = await api(`${pollPath(channelId, messageId)}/votes`, { method: 'PUT', json: { option_ids: optionIds }, decode: decodePollDetail })
  assertCommunity(cx, community)
  applyPoll(cx, messageId, tallies(detail))
  return detail
}

/** Ends the poll now (its author, or Manage messages). */
export async function endPoll(cx: Cx, channelId: string, messageId: string): Promise<PollDetail> {
  const community = cx.cs.communityGeneration
  const detail = await api(`${pollPath(channelId, messageId)}/end`, { method: 'POST', decode: decodePollDetail })
  assertCommunity(cx, community)
  applyPoll(cx, messageId, tallies(detail))
  return detail
}

/** Posts a poll to the channel; the WebSocket echo is de-duplicated by insertMessage. */
export async function createPoll(cx: Cx, channelId: string, request: PollCreateRequest) {
  const community = cx.cs.communityGeneration
  // The question is message text: a typed :name: becomes the emoji, like in any message. Options stay plain text.
  const json = { ...request, question: encodeShortcodes(request.question, useEmojiStore().byName) }
  const msg = await api(`/api/channels/${channelId}/polls`, { method: 'POST', json, decode: decodeMessage })
  assertCommunity(cx, community)
  insertMessage(cx, msg)
  return msg
}
