import { describe, it, expect, vi } from 'vitest'
import { createRtcState, type Cx } from './state'
import { retuneForCapture, syncLayers } from './layers'

function sender(track: unknown = null) {
  return {
    track,
    replaceTrack: vi.fn(() => Promise.reject(new Error('gone'))),
    getParameters: () => ({ encodings: [{}] }),
    setParameters: vi.fn(async () => {})
  } as unknown as RTCRtpSender
}

function room() {
  const st = createRtcState()
  const sendWSEvent = vi.fn()
  const cx = { voice: { screenQuality: {}, layersWanted: null }, chat: { sendWSEvent }, st, hooks: { filterFailed: vi.fn() } } as unknown as Cx
  return { cx, st, sendWSEvent }
}

describe('screen layers without a running share', () => {
  it('silences the lower layers, restores the best one and forgets what was declared', async () => {
    const { cx, st, sendWSEvent } = room()
    const layer = sender({ id: 'old' })
    st.layerSenders = [layer, null]
    st.screenSender = sender()
    st.declaredKey = 'was-declared'
    await syncLayers(cx)
    expect(layer.replaceTrack).toHaveBeenCalledWith(null)
    expect(st.screenSender.setParameters).toHaveBeenCalled()
    expect(st.declaredKey).toBe('')
    expect(sendWSEvent).not.toHaveBeenCalled()
  })

  it('does nothing where the server offers no layer lines', async () => {
    const { cx, st } = room()
    st.screenSender = sender()
    await syncLayers(cx)
    expect(st.screenSender.setParameters).not.toHaveBeenCalled()
  })
})

describe('layer changes that fail', () => {
  it('leave later quality changes running', async () => {
    const { cx, st } = room()
    st.screenSender = sender()
    st.layerSenders = [sender(), null]
    const broken = { getSettings: () => { throw new Error('capture lost') } } as unknown as MediaStreamTrack
    await expect(retuneForCapture(cx, broken)).resolves.toBeUndefined()
    st.localScreenStream.value = { getVideoTracks: () => [broken] } as unknown as MediaStream
    await expect(syncLayers(cx)).resolves.toBeUndefined()
    st.localScreenStream.value = null
    await syncLayers(cx)
    expect(st.screenSender.setParameters).toHaveBeenCalled()
  })
})
