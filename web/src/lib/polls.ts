// Polls: the limits the server enforces (internal/poll), and the pure rules the
// poll card and the creation dialog share.
import type { Poll, PollCreateRequest } from '../types/domain'

export const POLL_DURATIONS = ['1h', '4h', '8h', '24h', '3d', '1w'] as const
export type PollDuration = (typeof POLL_DURATIONS)[number]
export const DEFAULT_POLL_DURATION: PollDuration = '24h'
export const POLL_MIN_OPTIONS = 2
export const POLL_MAX_OPTIONS = 10
export const POLL_MAX_QUESTION = 300
export const POLL_MAX_LABEL = 55

export function isPollDuration(value: unknown): value is PollDuration {
  return POLL_DURATIONS.some(d => d === value)
}

/** Voting is over: the server said so, or the end time has passed on this clock. */
export function isEnded(poll: Poll, now: number): boolean {
  return poll.closed || Date.parse(poll.closes_at) <= now
}

/** Share of the voters who chose an option, as a whole percent (several answers can add up to more than 100). */
export function percent(votes: number, voters: number): number {
  return voters > 0 ? Math.round((votes * 100) / voters) : 0
}

export interface Remaining { unit: 'minute' | 'hour' | 'day'; count: number }

/** How long voting stays open, rounded up to the unit that reads best; null once it ended. */
export function remaining(poll: Poll, now: number): Remaining | null {
  const ms = Date.parse(poll.closes_at) - now
  if (poll.closed || !(ms > 0)) return null
  const minutes = Math.ceil(ms / 60_000)
  if (minutes < 60) return { unit: 'minute', count: minutes }
  const hours = Math.ceil(minutes / 60)
  if (hours < 48) return { unit: 'hour', count: hours }
  return { unit: 'day', count: Math.ceil(hours / 24) }
}

/** The options a member voted for, when the poll names its voters. */
export function votedFor(poll: Poll, userId: string | undefined): string[] {
  if (!userId) return []
  return poll.options.filter(o => o.voters?.includes(userId)).map(o => o.id)
}

/** What the member's votes become when they click an option. */
export function nextSelection(current: readonly string[], optionId: string, multiple: boolean): string[] {
  if (current.includes(optionId)) return current.filter(id => id !== optionId)
  return multiple ? [...current, optionId] : [optionId]
}

/** The options with the most votes (none while nobody voted). */
export function leaders(poll: Poll): string[] {
  const top = Math.max(0, ...poll.options.map(o => o.votes))
  return top === 0 ? [] : poll.options.filter(o => o.votes === top).map(o => o.id)
}

export interface PollDraft {
  question: string
  options: string[]
  duration: PollDuration
  multiple: boolean
  anonymous: boolean
}

export function emptyDraft(): PollDraft {
  return { question: '', options: ['', ''], duration: DEFAULT_POLL_DURATION, multiple: false, anonymous: false }
}

export type DraftProblem = 'question' | 'options' | 'duplicate' | null

/** What stops a draft from being posted: no question, fewer than two answers, or equal answers. */
export function draftProblem(draft: PollDraft): DraftProblem {
  if (!draft.question.trim()) return 'question'
  const labels = filledOptions(draft)
  if (labels.length < POLL_MIN_OPTIONS) return 'options'
  if (new Set(labels.map(l => l.toLowerCase())).size !== labels.length) return 'duplicate'
  return null
}

function filledOptions(draft: PollDraft): string[] {
  return draft.options.map(o => o.trim()).filter(Boolean)
}

/** The request for a valid draft; empty answer fields are left out. */
export function draftRequest(draft: PollDraft): PollCreateRequest {
  return {
    question: draft.question.trim(),
    options: filledOptions(draft),
    duration: draft.duration,
    multiple: draft.multiple,
    anonymous: draft.anonymous
  }
}
