import { describe, it, expect, vi } from 'vitest'
import { present } from '../../media-test.fixture'
import { CAPTURE_POLL_MS, CAPTURE_SETTLE_MS } from './useWebRTC'
import { useChatStore } from '../../stores/chat'
import { installHarness, FakePC, SFU_LAYER_OFFER, SFU_OFFER, declared, declaredLayersOf, display, encodingOf, installRecordingCall, joinedWithLayers, stubDisplayMedia } from './useWebRTC.harness.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

/** The copies the code made of a capture, in order. */
const clonesOf = (track: MediaStreamTrack) => vi.mocked(track.clone).mock.results.map(r => r.value as MediaStreamTrack)

describe('layers of my screen share', () => {
  it('binds the lower layers on the lines the server offers, with their layer in the seal', async () => {
    const call = installRecordingCall()
    const { pc } = await joinedWithLayers()
    expect(pc.transceivers.map(t => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendrecv', 'sendrecv', 'sendrecv', 'sendrecv'])
    // Mic, screen (layer 0), camera, screen sound, then layers 1 and 2.
    // Each line also says what it carries: 0 microphone, 1 camera, 2 screen, 3 screen sound.
    expect(call.protectSender.mock.calls.map(c => [(c as unknown[])[1], (c as unknown[])[2]])).toEqual([[0, undefined], [2, 0], [1, undefined], [3, undefined], [2, 1], [2, 2]])
  })

  it('leaves the lines alone when the server does not take layers', async () => {
    const call = installRecordingCall()
    const { pc, rtc, sent } = await joinedWithLayers({ announce: 0 })
    expect(pc.transceivers.map(t => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendrecv', 'sendrecv', 'recvonly', 'recvonly'])
    expect(call.protectSender).toHaveBeenCalledTimes(4)
    const { stream } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(declared(sent)).toEqual([])
    expect(present(pc.senders[4]).track).toBeNull()
  })

  it('sends a 4K share as three layers and declares them, only the best sent until viewers need more', async () => {
    const { rtc, sent, low1, low2, screen } = await joinedWithLayers()
    const { stream, track } = display(3840, 2160)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    // Each lower line carries its own clone of the capture; the best line keeps the capture itself.
    const [screenCopy1, screenCopy2] = clonesOf(track)
    expect(low1.track).toBe(screenCopy1)
    expect(low2.track).toBe(screenCopy2)
    expect(screen.track).toBe(track)
    expect(declared(sent)).toEqual([{ layers: [{ width: 1920, height: 1080, fps: 30 }, { width: 1280, height: 720, fps: 30 }, { width: 852, height: 480, fps: 30 }] }])
    // The best layer is the share as chosen (1080p30 of a 4K capture, scaled by the encoder).
    expect(encodingOf(screen)).toMatchObject({ active: true, scaleResolutionDownBy: 2, maxFramerate: 30 })
    // The lower layers scale the same capture further and wait until they are wanted.
    expect(encodingOf(low1)).toMatchObject({ active: false, scaleResolutionDownBy: 3, maxFramerate: 30 })
    expect(encodingOf(low2)).toMatchObject({ active: false, scaleResolutionDownBy: 4.5, maxFramerate: 30 })
    expect(present(encodingOf(low1).maxBitrate)).toBeLessThan(present(encodingOf(screen).maxBitrate))
    expect(present(encodingOf(low2).maxBitrate)).toBeLessThan(present(encodingOf(low1).maxBitrate))
  })

  it('sends only the layers some viewer needs', async () => {
    const { rtc, voice, low1, low2, screen } = await joinedWithLayers()
    stubDisplayMedia(display(3840, 2160).stream)
    await rtc.startScreenShare()

    voice.layersWanted = [0, 2]
    await vi.waitFor(() => expect(encodingOf(low2).active).toBe(true))
    expect(encodingOf(screen).active).toBe(true)
    expect(encodingOf(low1).active).toBe(false)

    // Everybody moved to the lower layers: the best one is not encoded at all.
    voice.layersWanted = [1, 2]
    await vi.waitFor(() => expect(encodingOf(screen).active).toBe(false))
    expect(encodingOf(low1).active).toBe(true)
    expect(encodingOf(low2).active).toBe(true)
  })

  it('stopping the share empties the layers, forgets what was wanted and declares again next time', async () => {
    const { rtc, voice, sent, low1, low2, screen } = await joinedWithLayers()
    stubDisplayMedia(display(1920, 1080).stream)
    await rtc.startScreenShare()
    voice.layersWanted = [1]
    await vi.waitFor(() => expect(encodingOf(low1).active).toBe(true))
    expect(encodingOf(screen).active).toBe(false)

    rtc.stopScreenShare()
    await vi.waitFor(() => expect(low1.track).toBeNull())
    expect(low2.track).toBeNull()
    expect(voice.layersWanted).toBeNull()
    // The next share starts on its best layer.
    await vi.waitFor(() => expect(encodingOf(screen).active).toBe(true))

    const { stream, track } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    // The lower line carries its own clone of the new capture.
    expect(low1.track).toBe(clonesOf(track)[0])
    expect(declared(sent)).toHaveLength(2)
    expect(encodingOf(low1).active).toBe(false)
  })

  it('re-declares and re-tunes when the quality changes, and drops layers that are no longer worth sending', async () => {
    const { rtc, voice, sent, low1, low2 } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    track.applyConstraints = vi.fn(async () => {})
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(declared(sent)).toHaveLength(1)
    // The lower line carries a clone; it goes null once the capture shrinks below the layer threshold.
    expect(low2.track).toBe(clonesOf(track)[1])

    // The capture is 360p now: nothing smaller to send.
    track.getSettings = () => ({ width: 640, height: 360, frameRate: 30 })
    voice.setScreenQuality({ resolution: 720, fps: 30 })
    await vi.waitFor(() => expect(declared(sent)).toHaveLength(2))
    expect(declared(sent)[1]).toEqual({ layers: [{ width: 640, height: 360, fps: 30 }] })
    expect(low1.track).toBeNull()
    expect(low2.track).toBeNull()

    // Unchanged layers are not declared twice.
    voice.setScreenQuality({ resolution: 720, fps: 30, custom: true })
    await new Promise(r => setTimeout(r, 10))
    expect(declared(sent)).toHaveLength(2)
  })

  it('does not declare layers while the capture size is unknown', async () => {
    const { rtc, sent, low1 } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    track.getSettings = () => ({})
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(declared(sent)).toEqual([])
    expect(low1.track).toBeNull()
  })

  describe('when the captured window changes size', () => {
    async function sharing() {
      const ctx = await joinedWithLayers()
      vi.useFakeTimers()
      const shown = display(1920, 1080)
      stubDisplayMedia(shown.stream)
      await ctx.rtc.startScreenShare()
      expect(declared(ctx.sent)).toHaveLength(1)
      const resize = (width: number, height: number, frameRate = 30) => {
        shown.track.getSettings = () => ({ width, height, frameRate })
      }
      return { ...ctx, resize }
    }

    it('declares the layers again, and sizes the best layer for the new picture', async () => {
      const { sent, screen, resize } = await sharing()
      expect(encodingOf(screen)).toMatchObject({ scaleResolutionDownBy: 1 })
      resize(1280, 720)
      // Nothing happens before the change has held.
      await vi.advanceTimersByTimeAsync(CAPTURE_SETTLE_MS - 100)
      expect(declared(sent)).toHaveLength(1)
      await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + CAPTURE_SETTLE_MS)
      expect(declared(sent)).toHaveLength(2)
      expect(declaredLayersOf(sent, 1).map(l => l.height)).toEqual([720, 480, 360])
      // A bigger picture is scaled down to the chosen quality again.
      resize(3840, 2160)
      await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + CAPTURE_SETTLE_MS)
      expect(declared(sent)).toHaveLength(3)
      expect(encodingOf(screen)).toMatchObject({ scaleResolutionDownBy: 2 })
    })

    it('waits until the size stops changing', async () => {
      const { sent, resize } = await sharing()
      for (const [w, h] of [[1700, 1000], [1500, 900], [1280, 720]] as const) {
        resize(w, h)
        await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + 100)
      }
      expect(declared(sent)).toHaveLength(1)
      await vi.advanceTimersByTimeAsync(CAPTURE_SETTLE_MS + CAPTURE_POLL_MS)
      expect(declared(sent)).toHaveLength(2)
      expect(declaredLayersOf(sent, 1)[0]).toMatchObject({ width: 1280, height: 720 })
    })

    it('re-tunes for a change of the frame rate and ignores noise below a whole frame', async () => {
      const { sent, screen, resize } = await sharing()
      const tuned = screen.setParameters.mock.calls.length
      resize(1920, 1080, 30.4)
      await vi.advanceTimersByTimeAsync(3 * CAPTURE_SETTLE_MS)
      expect(screen.setParameters.mock.calls.length).toBe(tuned)
      resize(1920, 1080, 24)
      await vi.advanceTimersByTimeAsync(3 * CAPTURE_SETTLE_MS)
      expect(screen.setParameters.mock.calls.length).toBeGreaterThan(tuned)
      // The declared layers are the chosen quality's, which did not change.
      expect(declared(sent)).toHaveLength(1)
    })

    it('stops watching when the share stops', async () => {
      const { rtc, sent, resize } = await sharing()
      rtc.stopScreenShare()
      resize(1280, 720)
      await vi.advanceTimersByTimeAsync(5 * CAPTURE_SETTLE_MS)
      expect(declared(sent)).toHaveLength(1)
      // A change that was waiting to settle is dropped with the share.
      const again = display(1920, 1080)
      stubDisplayMedia(again.stream)
      await rtc.startScreenShare()
      again.track.getSettings = () => ({ width: 800, height: 600, frameRate: 30 })
      await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + 100)
      rtc.stopScreenShare()
      await vi.advanceTimersByTimeAsync(5 * CAPTURE_SETTLE_MS)
      expect(declared(sent)).toHaveLength(2)
    })
  })

  it('a reconnect restores the layers on the new connection', async () => {
    const { rtc, voice, sent } = await joinedWithLayers()
    stubDisplayMedia(display(1920, 1080).stream)
    await rtc.startScreenShare()
    voice.serverScreenLayers = 3
    rtc.rejoinAfterReconnect()
    const pc = present(FakePC.instances.at(-1))
    useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_LAYER_OFFER } })
    await vi.waitFor(() => expect(pc.transceivers.length).toBe(6))
    const low1 = present(pc.senders[4])
    await vi.waitFor(() => expect(low1.track).not.toBeNull())
    expect(encodingOf(low1)).toMatchObject({ active: false })
    expect(declared(sent)).toHaveLength(2)
  })

  it('sends only the layer lines the server announced', async () => {
    const { rtc, low1, low2, sent } = await joinedWithLayers({ announce: 2 })
    const { stream, track } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    // One layer line: the second layer of the plan has no sender.
    expect(low1.track).toBe(clonesOf(track)[0])
    expect(low2.track).toBeNull()
    expect(declared(sent)).toHaveLength(1)
  })

  it('a share that is stopped while its layers are being set up is not resumed', async () => {
    const { rtc, low1, low2 } = await joinedWithLayers()
    const { stream } = display(1920, 1080)
    stubDisplayMedia(stream)
    low1.setParameters.mockImplementation(async () => { rtc.stopScreenShare() })
    await rtc.startScreenShare()
    await vi.waitFor(() => expect(low1.track).toBeNull())
    expect(low2.track).toBeNull()
  })

  it('does not plan layers for a track that cannot tell its size', async () => {
    const { rtc, low1, sent } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    Reflect.deleteProperty(track, 'getSettings')
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    expect(declared(sent)).toEqual([])
    expect(low1.track).toBeNull()
  })

  it('leaving the call during a layer update leaves the lower senders alone', async () => {
    const { rtc, low1 } = await joinedWithLayers()
    stubDisplayMedia(display(1920, 1080).stream)
    const sharing = rtc.startScreenShare()
    rtc.leaveVoiceChannel()
    await sharing
    expect(low1.track).toBeNull()
  })

  // The browser adapts one track to the most constrained sender it feeds, so
  // sharing the capture track across all lower lines causes a bandwidth-limited
  // best layer to also shrink the thumbnails.  Each lower line must encode its
  // own clone so it can adapt independently.
  it('gives each lower screen line its own clone of the capture, distinct per line, with contentHint kept', async () => {
    const { rtc, low1, low2, screen } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    track.contentHint = 'detail'
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    const [copy1, copy2] = clonesOf(track)
    expect(copy1).not.toBe(copy2)
    // The best line keeps the capture itself; lower lines each get their own copy.
    expect(screen.track).toBe(track)
    expect(low1.track).toBe(copy1)
    expect(low2.track).toBe(copy2)
    expect(copy1?.contentHint).toBe('detail')
    expect(copy2?.contentHint).toBe('detail')
  })

  // A clone keeps the hint and the constraints the capture had when it was made:
  // a quality change ("optimise for", size, rate) gives the lower lines new copies.
  it('gives the lower lines new copies with the new hint when the share switches to motion', async () => {
    const { rtc, voice, low1, low2 } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()
    const [old1, old2] = clonesOf(track)
    expect(old1?.contentHint).toBe('detail')

    voice.setScreenQuality({ resolution: 1080, fps: 30, optimize: 'motion' })
    await vi.waitFor(() => expect(track.contentHint).toBe('motion'))
    expect(old1?.stop).toHaveBeenCalled()
    expect(old2?.stop).toHaveBeenCalled()
    await vi.waitFor(() => expect(low1.track?.contentHint).toBe('motion'))
    expect(low2.track?.contentHint).toBe('motion')
    expect([low1.track, low2.track]).not.toContain(old1)
  })

  it('reuses the copies across syncs while the capture is the same', async () => {
    const { rtc, voice, low1, low2 } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    const [copy1, copy2] = clonesOf(track)

    voice.layersWanted = [0, 2]
    await vi.waitFor(() => expect(low2.track).toBe(copy2))
    // No new clone was made; the existing ones are reused.
    expect(clonesOf(track)).toHaveLength(2)
    expect(low1.track).toBe(copy1)
    expect(low2.track).toBe(copy2)
  })

  it('stopScreenShare stops every screen copy before it returns, synchronously', async () => {
    const { rtc, low1, low2 } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    const [copy1, copy2] = clonesOf(track)
    // Copies are live while sharing.
    expect(copy1?.stop).not.toHaveBeenCalled()

    rtc.stopScreenShare()
    // Synchronously stopped: no await needed.
    expect(copy1?.stop).toHaveBeenCalled()
    expect(copy2?.stop).toHaveBeenCalled()
    // The senders are cleared (possibly async via syncLayers).
    await vi.waitFor(() => expect(low1.track).toBeNull())
    expect(low2.track).toBeNull()
  })

  it('switching the source (replaceScreenShare) stops old copies and makes new ones', async () => {
    const { rtc, low1 } = await joinedWithLayers()
    const first = display(1920, 1080)
    stubDisplayMedia(first.stream)
    await rtc.startScreenShare()

    const [oldCopy1] = clonesOf(first.track)
    expect(low1.track).toBe(oldCopy1)

    // Replace with a new source.
    const second = display(1920, 1080)
    stubDisplayMedia(second.stream)
    const { pendingConfirm } = await import('../../shared/lib/confirm')
    const start = rtc.startScreenShare()
    await vi.waitFor(() => expect(pendingConfirm.value).not.toBeNull())
    present(pendingConfirm.value).resolve(true)
    await start

    // Old clones were stopped when the source was replaced.
    expect(oldCopy1?.stop).toHaveBeenCalled()
    // New clones are made from the second capture.
    await vi.waitFor(() => expect(low1.track).not.toBeNull())
    expect(low1.track).toBe(clonesOf(second.track)[0])
    expect(low1.track).not.toBe(first.track)
  })

  it('retuneForCapture recreates the copies so they reflect the new capture settings', async () => {
    const ctx = await joinedWithLayers()
    vi.useFakeTimers()
    const { rtc, low1 } = ctx
    const shown = display(1920, 1080)
    stubDisplayMedia(shown.stream)
    await rtc.startScreenShare()

    const [oldCopy1, oldCopy2] = clonesOf(shown.track)
    expect(low1.track).toBe(oldCopy1)

    // Simulate a captured window resize that triggers retuneForCapture.
    shown.track.getSettings = () => ({ width: 1280, height: 720, frameRate: 30 })
    await vi.advanceTimersByTimeAsync(CAPTURE_POLL_MS + CAPTURE_SETTLE_MS)

    // Old clones stopped; new ones made from the resized capture.
    expect(oldCopy1?.stop).toHaveBeenCalled()
    expect(oldCopy2?.stop).toHaveBeenCalled()
    await vi.waitFor(() => expect(low1.track).not.toBeNull())
    expect(low1.track).not.toBe(oldCopy1)
  })

  it('stops clones of screen lines that have no sender after a reconnect', async () => {
    const { rtc, voice, chat } = await joinedWithLayers()
    const { stream, track } = display(1920, 1080)
    stubDisplayMedia(stream)
    await rtc.startScreenShare()

    const [copy1, copy2] = clonesOf(track)

    // Reconnect with an offer that has no screen layer lines (only the four fixed lines).
    voice.serverScreenLayers = 0
    rtc.rejoinAfterReconnect()
    const pc = present(FakePC.instances.at(-1))
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(pc.transceivers.length).toBe(4))

    // The new connection has no screen layer senders: existing clones must be stopped.
    await vi.waitFor(() =>
      expect(copy1?.stop).toHaveBeenCalled()
    )
    expect(copy2?.stop).toHaveBeenCalled()
  })
})
