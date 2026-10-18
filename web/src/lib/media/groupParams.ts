import {
  type KeyPackage,
  type PrivateKeyPackage,
} from 'ts-mls'
import { type EpochKeys } from './frame'
import { type MediaVerification } from './verify'

// A member's leaf is swapped at most once per this long, doubling while it keeps happening.
export const SWAP_BASE_MS = 3000
// A group is thrown away (a missed commit, a restarted committer, a removal)
// at most once per this long, doubling while it keeps happening.
export const RESET_BASE_MS = 10_000
export const BACKOFF_MAX_MS = 60_000
// A welcome from a lower id outside my group, built on my own key package, may
// make me drop a group that others depend on at most once per this long per
// sender, doubling while it keeps happening.
export const RIVAL_BASE_MS = 60_000
export const RIVAL_MAX_MS = 600_000
// A joiner whose offer got no welcome in this long offers again.
export const OFFER_WAIT_MS = 4000
// The lowest joiner whose offer got no welcome in this long founds the group:
// nobody here holds one. A welcome that was still on its way is taken anyway
// (see the welcome below), so founding early costs nothing.
export const FOUND_WAIT_MS = 4000
// A member lower than me that the room lists but never said a word in this
// long cannot be speaking MLS (an old tab, a failed start, someone whose
// reload is still under way): the next one acts. Coming back, it offers and
// is let in. It stays longer than OFFER_WAIT_MS, so a lost welcome is asked
// for again before the member who sent it is written off.
export const TAKEOVER_MS = 2 * OFFER_WAIT_MS
// How long the state before a member's self-update is kept: a commit that
// raced it for the same epoch and wins replaces it (see `selfUpdate`).
export const CONFLICT_MS = 10_000

export interface MediaStep {
  wires: Uint8Array[]
  epoch: number | null
  key: EpochKeys | null
  /** What members compare to know they hold the same keys; set with the key. */
  verification?: MediaVerification | null
  /**
   * The epoch was reached by a commit that removed nobody: the sender may go on
   * sealing under the epoch it left for a moment (SEND_GRACE_MS).
   */
  soft?: boolean
  /** Members of the epoch known to open frame version 3, and whether that is all of them. */
  caps?: { members: string[], complete: boolean }
}

export interface OwnPackage {
  publicPackage: KeyPackage
  privatePackage: PrivateKeyPackage
}
