// The clock of the key lifecycle. Epoch ages, liveness and the previous-epoch
// deadlines are measured with the monotonic clock, so a wall-clock step (NTP,
// a manual change, a timezone fix) can neither rotate keys early nor keep an
// old epoch alive. The origin is added only so the numbers do not restart at
// zero in every page: a heartbeat's time must keep growing across a reload.
// Tests replace `clock.now` with a stepped clock.

export function monotonicNow(): number {
  return typeof performance === 'object' ? performance.timeOrigin + performance.now() : Date.now()
}

export const clock = {
  now: monotonicNow,
  /** The wall clock: compared with `now` only to notice that the machine slept while the monotonic clock stood still. */
  wall: (): number => Date.now(),
}
