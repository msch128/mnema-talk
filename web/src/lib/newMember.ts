// Raid protection from a new member's side (doc/moderation.md): when the
// server lets them write, react and speak, and how that reads.
import { durationLabel } from './moderation'
import { t } from '../i18n'

export type NewMemberAction = 'write' | 'react' | 'speak'

/** The moments (ms since the epoch) at which each held-back action opens up; null: not held back. */
export type NewMemberTimes = Readonly<Record<NewMemberAction, number | null>>

export const NO_WAIT: NewMemberTimes = { write: null, react: null, speak: null }

function at(value: string | undefined): number | null {
  const ms = value === undefined ? NaN : Date.parse(value)
  return Number.isNaN(ms) ? null : ms
}

/** The times of the server's `new_member` object. */
export function newMemberTimes(info: { write_at?: string | undefined; react_at?: string | undefined; speak_at?: string | undefined } | undefined): NewMemberTimes {
  return info ? { write: at(info.write_at), react: at(info.react_at), speak: at(info.speak_at) } : NO_WAIT
}

/** Milliseconds still to wait for an action at `now`; 0 when it is open. */
export function waitLeft(times: NewMemberTimes, action: NewMemberAction, now: number): number {
  const when = times[action]
  return when === null ? 0 : Math.max(0, when - now)
}

/** The earliest moment after `now` at which something opens up, or null. */
export function nextOpening(times: NewMemberTimes, now: number): number | null {
  const pending = Object.values(times).filter((when): when is number => when !== null && when > now)
  return pending.length ? Math.min(...pending) : null
}

/** "You can write in 12 min": the calm sentence for a held-back action, rounded up to whole minutes from a minute on. */
export function waitSentence(action: NewMemberAction, ms: number): string {
  const seconds = Math.ceil(ms / 1000)
  const rounded = seconds > 60 ? Math.ceil(seconds / 60) * 60 : seconds
  return t(`newMember.${action}`, { time: durationLabel(rounded) })
}
