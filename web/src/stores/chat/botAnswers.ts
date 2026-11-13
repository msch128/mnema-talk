// What a bot did in a channel, for a component that waits for its answer (doc/bots.md,
// Message components). A click is answered by one of four events, all of which
// name the bot and the channel:
//   - message_update of the message the component is on (callback 7, or the
//     deferred update after 6),
//   - message_create by the bot in that channel (callback 4, or the follow-up after 5),
//   - interaction_response from the bot (a private note, also the one the
//     component is on being replaced, callback 4 or 7 with flags 64),
//   - interaction_modal from the bot (callback 9).
// None of them carries the interaction id, and messages name the bot's account
// (user_id) while notes and forms name the bot (bot_id), so the one key all four
// share is the bot's name. The waiting component therefore matches by channel and
// by the message or note it is on, or else by the bot's name. Two bots of the same
// name in one channel, or a second click answered first, end the wait a little
// early: that only stops the "working" line sooner, it never hides an answer.
import type { Ref } from 'vue'

export interface BotAnswer {
  seq: number
  channelId: string
  /** The message or note the answer changed, if it changed one. */
  messageId?: string
  botName: string
}

export interface BotAnswerState { botAnswer: Ref<BotAnswer | null> }

/** Records that a bot answered something in a channel. */
export function noteBotAnswer(cs: BotAnswerState, answer: Omit<BotAnswer, 'seq'>): void {
  cs.botAnswer.value = { ...answer, seq: (cs.botAnswer.value?.seq ?? 0) + 1 }
}

/** The bot answer of a message event, or null for a member's message. */
export function messageAnswer(m: { is_bot?: boolean; is_webhook?: boolean; channel_id: string; id: string; display_name?: string; username?: string }, changed: boolean): Omit<BotAnswer, 'seq'> | null {
  if (!m.is_bot || m.is_webhook) return null
  const botName = m.display_name || m.username || ''
  return changed ? { channelId: m.channel_id, messageId: m.id, botName } : { channelId: m.channel_id, botName }
}
