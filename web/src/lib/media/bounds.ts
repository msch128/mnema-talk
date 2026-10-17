// One handshake must survive base64 inside a 64 KiB WebSocket frame.
// The server rejects anything larger than HANDSHAKE_MAX_BYTES.
export const HANDSHAKE_MAX_BYTES = 32 * 1024
export const HANDSHAKE_MAX_CHARS = 48_000

// How long the keys of the group a member held before the call regrouped
// still open frames (see mediaFrameWorker).
export const PREVIOUS_GROUP_MS = 30_000
// How long a member without a group (regrouping, or removed and waiting to be
// let in again) goes on sealing with its last keys before it sends nothing.
export const STALE_SEAL_MS = 10_000

// Time-based key lifecycle (see SECURITY.md, "Key lifetime").
// The committer advances the epoch with a path commit once the epoch is this
// old, even if nobody joined or left, so a key never lives longer than this.
export const ROTATE_MS = 30 * 60_000
// Each epoch draws its own delay in ROTATE_MS +- this, so Talks do not rotate in lockstep.
export const ROTATE_JITTER_MS = 2 * 60_000
// A member that showed no sign of life for this long is removed from the group.
export const ABSENT_MS = 20 * 60_000
// Every member signs one tiny liveness message this often (a quarter of ABSENT_MS).
export const HEARTBEAT_MS = 5 * 60_000
// How often the group looks at its clocks.
export const MAINTENANCE_MS = 30_000

export interface MediaTiming {
  rotateMs: number
  rotateJitterMs: number
  absentMs: number
  heartbeatMs: number
  maintenanceMs: number
}

export const DEFAULT_TIMING: MediaTiming = {
  rotateMs: ROTATE_MS,
  rotateJitterMs: ROTATE_JITTER_MS,
  absentMs: ABSENT_MS,
  heartbeatMs: HEARTBEAT_MS,
  maintenanceMs: MAINTENANCE_MS,
}

export const MEDIA_EXPORTER_LABEL = 'mnema-media-v1'
export const FRAME_EPOCH_BYTES = 16

const USER_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

/** Lowercase voice-member id. Anything else is not a group member. */
export function mediaUserId(value: string): string | null {
  const id = value.trim().toLowerCase()
  return USER_ID.test(id) ? id : null
}

/** 16-byte id used as the group id, so one channel cannot open another. */
export function mediaGroupId(channelId: string): Uint8Array | null {
  const id = mediaUserId(channelId)
  if (!id) return null
  const hex = id.replace(/-/g, '')
  const out = new Uint8Array(16)
  for (let i = 0; i < 16; i++) out[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16)
  return out
}
