import type { Poll, PollCreateRequest, PollDetail } from '../../types/domain'
import { decodeMessage, decodePollDetail } from '../../types/domain'
import { api } from '../../lib/api'
import { assertCommunity, type Cx } from './state'
import { applyToMessage, insertMessage } from './messageWindow'

/** The poll of a message as the server last told it (no one's own votes). */
export function applyPoll(cx: Cx, messageId: string, poll: Poll) {
  applyToMessage(cx, messageId, m => { m.poll = poll })
}

function tallies({ my_votes, ...poll }: PollDetail): Poll {
  void my_votes
  return poll
}

function pollPath(channelId: string, messageId: string) {
  return `/api/channels/${channelId}/messages/${messageId}/poll`
}

/** The poll with the caller's own votes, the only place an anonymous poll shows them. */
export async function fetchPoll(cx: Cx, channelId: string, messageId: string): Promise<PollDetail> {
  const community = cx.cs.communityGeneration
  const detail = await api(pollPath(channelId, messageId), { decode: decodePollDetail })
  assertCommunity(cx, community)
  applyPoll(cx, messageId, tallies(detail))
  return detail
}

/** Replaces the caller's votes (an empty list withdraws them). */
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
  const msg = await api(`/api/channels/${channelId}/polls`, { method: 'POST', json: request, decode: decodeMessage })
  assertCommunity(cx, community)
  insertMessage(cx, msg)
  return msg
}
