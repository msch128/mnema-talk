import { describe, it, expect } from 'vitest'
import { createMeteringTrack } from './micMetering'

import { TestTrack, TestStream, present } from '../media-test.fixture'

function fakeTrack() { return new TestTrack() }

describe('createMeteringTrack', () => {
  it('meters a clone, so gating the sent track does not silence the meter', () => {
    const sent = fakeTrack()
    const metering = createMeteringTrack(new TestStream([sent]))

    sent.enabled = false // noise gate closes / user mutes
    expect(metering).not.toBe(sent)
    expect(present(metering).enabled).toBe(true)
  })

  it('enables the clone even if the source was gated when cloned', () => {
    const sent = fakeTrack()
    sent.enabled = false
    expect(present(createMeteringTrack(new TestStream([sent]))).enabled).toBe(true)
  })

  it('returns null without an audio track', () => {
    expect(createMeteringTrack(new TestStream())).toBeNull()
    expect(createMeteringTrack(null)).toBeNull()
  })
})
