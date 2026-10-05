// The noise gate and mute switch the transmitted mic track on and off
// (track.enabled). A disabled track feeds silence into Web Audio, so a meter
// on that same track reads 0 % as soon as the gate closes: the gate never
// reopens and the settings meter shows nothing to calibrate against.
// Metering therefore runs on a clone that is always enabled and shares the
// same capture source.

/** Returns an always-enabled clone of the stream's mic track, or null. */
export function createMeteringTrack(stream) {
  const track = stream?.getAudioTracks()[0]
  if (!track) return null
  const clone = track.clone()
  clone.enabled = true
  return clone
}
