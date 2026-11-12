// The clock of the key lifecycle. Epoch ages, liveness and the previous-epoch
// deadlines are measured with the monotonic clock, so a wall-clock step (NTP,
// a manual change, a timezone fix) can neither rotate keys early nor keep an
// old epoch alive. The group's back-offs (backoff.ts) and the pacer of
// handshakes (pacer.ts) wait on it too, so a step back can neither keep a
// back-off shut nor hold back handshakes for the size of the step. Whether it
// advances while the machine sleeps depends on the browser: both are safe for
// them (a wait that stood still ends at most its own length after waking, one
// that jumped is over, as the time really passed). The origin is added only so the numbers do not restart at
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
