import { describe, it, expect, vi } from 'vitest'
import { createMeteringTrack } from './micMetering'

function fakeTrack() {
  const track = { enabled: true, stop: vi.fn() }
  track.clone = vi.fn(() => ({ ...fakeTrack(), enabled: track.enabled }))
  return track
}

describe('createMeteringTrack', () => {
  it('meters a clone, so gating the sent track does not silence the meter', () => {
    const sent = fakeTrack()
    const metering = createMeteringTrack({ getAudioTracks: () => [sent] })

    sent.enabled = false // noise gate closes / user mutes
    expect(metering).not.toBe(sent)
    expect(metering.enabled).toBe(true)
  })

  it('enables the clone even if the source was gated when cloned', () => {
    const sent = fakeTrack()
    sent.enabled = false
    expect(createMeteringTrack({ getAudioTracks: () => [sent] }).enabled).toBe(true)
  })

  it('returns null without an audio track', () => {
    expect(createMeteringTrack({ getAudioTracks: () => [] })).toBeNull()
    expect(createMeteringTrack(null)).toBeNull()
  })
})
