import { describe, it, expect, vi } from 'vitest'
import { present } from '../media-test.fixture'
import { installHarness, cameraCapture, declared, declaredLayersOf, encodingOf, installRecordingCall, joinedWithCameraLayers, joined, FakePC, SFU_CAMERA_LAYER_OFFER } from './useWebRTC.harness.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

const cameraDeclarations = (sent: Parameters<typeof declared>[0]) => declared(sent).filter(p => (p as { source?: string }).source === 'camera')

function cameraFrom(stream: MediaStream) {
  vi.mocked(navigator.mediaDevices.getUserMedia).mockImplementationOnce(async () => stream)
}

describe('layers of my camera', () => {
  it('binds the lower layers on the lines after the screen\'s, each sealed with its layer', async () => {
    const call = installRecordingCall()
    const { pc } = await joinedWithCameraLayers()
    expect(pc.transceivers).toHaveLength(8)
    // Mic, screen, camera, screen sound, the screen's layers 1 and 2, the camera's layers 1 and 2.
    // Each line says what it carries: 0 microphone, 1 camera, 2 screen, 3 screen sound.
    expect(call.protectSender.mock.calls.map(c => [(c as unknown[])[1], (c as unknown[])[2]])).toEqual(
      [[0, undefined], [2, 0], [1, 0], [3, undefined], [2, 1], [2, 2], [1, 1], [1, 2]]
    )
  })

  it('declares the layers before the first video and sends only the best until viewers need more', async () => {
    const { rtc, sent, camera, cameraLow1, cameraLow2 } = await joinedWithCameraLayers()
    const { stream, track } = cameraCapture(1280, 720)
    cameraFrom(stream)
    // What the server knew when the camera line got its video.
    const knownThen: number[] = []
    const attach = camera.replaceTrack.getMockImplementation()
    camera.replaceTrack.mockImplementation(async (t) => { knownThen.push(cameraDeclarations(sent).length); await attach?.(t) })

    await rtc.startCamera()

    expect(knownThen).toEqual([1])
    expect(cameraDeclarations(sent)).toEqual([{ layers: [{ width: 1280, height: 720, fps: 30 }, { width: 640, height: 360, fps: 30 }, { width: 320, height: 180, fps: 15 }], source: 'camera' }])
    expect(camera.track).toBe(track)
    // The lower lines carry the same capture, scaled down by the encoder, but stay off.
    expect(cameraLow1.track).toBe(track)
    expect(cameraLow2.track).toBe(track)
    expect(encodingOf(cameraLow1)).toMatchObject({ active: false, scaleResolutionDownBy: 2, maxFramerate: 30, maxBitrate: 500_000 })
    expect(encodingOf(cameraLow2)).toMatchObject({ active: false, scaleResolutionDownBy: 4, maxFramerate: 15, maxBitrate: 150_000 })
    expect(encodingOf(camera)).toMatchObject({ active: true })
    // The screen's layers are not the camera's.
    expect(declared(sent).filter(p => !(p as { source?: string }).source)).toEqual([])
  })

  it('follows the layers the server says viewers need, and the best one goes quiet when nobody needs it', async () => {
    const { rtc, chat, voice, camera, cameraLow1, cameraLow2 } = await joinedWithCameraLayers()
    cameraFrom(cameraCapture(1280, 720).stream)
    await rtc.startCamera()

    // A small tile and a thumbnail: layers 1 and 2; layer 0 stays on while someone else needs it.
    chat.handleWSEvent({ type: 'screen_layers_wanted', payload: { channel_id: 'ch-1', layers: [0, 2], source: 'camera' } })
    await vi.waitFor(() => expect(encodingOf(cameraLow2).active).toBe(true))
    expect(encodingOf(cameraLow1).active).toBe(false)
    expect(encodingOf(camera).active).toBe(true)
    expect(voice.cameraLayersWanted).toEqual([0, 2])
    // The screen's announcement is separate.
    expect(voice.layersWanted).toBeNull()

    chat.handleWSEvent({ type: 'screen_layers_wanted', payload: { channel_id: 'ch-1', layers: [2], source: 'camera' } })
    await vi.waitFor(() => expect(encodingOf(camera).active).toBe(false))
    expect(encodingOf(cameraLow2).active).toBe(true)

    // Someone's announcement for the screen never moves the camera's layers.
    chat.handleWSEvent({ type: 'screen_layers_wanted', payload: { channel_id: 'ch-1', layers: [0] } })
    expect(voice.cameraLayersWanted).toEqual([2])
    expect(voice.layersWanted).toEqual([0])
  })

  it('sends a camera that is too small for a second layer as one, declaring nothing', async () => {
    const { rtc, sent, cameraLow1, cameraLow2 } = await joinedWithCameraLayers()
    cameraFrom(cameraCapture(320, 180).stream)
    await rtc.startCamera()
    expect(cameraDeclarations(sent)).toEqual([])
    expect(cameraLow1.track).toBeNull()
    expect(cameraLow2.track).toBeNull()
  })

  it('sends one plain camera to a server that does not take camera layers, as before', async () => {
    const call = installRecordingCall()
    const { rtc, sent, camera, cameraLow1, cameraLow2 } = await joinedWithCameraLayers({ camera: 0 })
    // The camera line is sealed without a layer.
    expect(call.protectSender.mock.calls.map(c => [(c as unknown[])[1], (c as unknown[])[2]])).toContainEqual([1, undefined])
    expect(call.protectSender.mock.calls.filter(c => (c as unknown[])[1] === 1)).toHaveLength(1)
    const { stream, track } = cameraCapture(1280, 720)
    cameraFrom(stream)
    await rtc.startCamera()
    expect(camera.track).toBe(track)
    expect(cameraDeclarations(sent)).toEqual([])
    expect(cameraLow1.track).toBeNull()
    expect(cameraLow2.track).toBeNull()
    expect(encodingOf(camera)).not.toHaveProperty('active', false)
  })

  it('sends one plain camera in a plain offer without lines for layers', async () => {
    const { rtc, sent, camera } = await joined()
    const { stream, track } = cameraCapture(1280, 720)
    cameraFrom(stream)
    await rtc.startCamera()
    expect(camera.track).toBe(track)
    expect(cameraDeclarations(sent)).toEqual([])
  })

  it('stops the lower layers with the camera and declares afresh for the next one', async () => {
    const { rtc, voice, chat, sent, cameraLow1, cameraLow2 } = await joinedWithCameraLayers()
    cameraFrom(cameraCapture(1280, 720).stream)
    await rtc.startCamera()
    chat.handleWSEvent({ type: 'screen_layers_wanted', payload: { channel_id: 'ch-1', layers: [0, 1, 2], source: 'camera' } })
    await vi.waitFor(() => expect(encodingOf(cameraLow1).active).toBe(true))

    rtc.stopCamera()
    expect(cameraLow1.track).toBeNull()
    expect(cameraLow2.track).toBeNull()
    expect(voice.cameraLayersWanted).toBeNull()
    expect(sent.some(e => e.type === 'webrtc_camera_stop')).toBe(true)

    cameraFrom(cameraCapture(1280, 720).stream)
    await rtc.startCamera()
    expect(cameraDeclarations(sent)).toHaveLength(2)
    expect(declaredLayersOf(sent, 1)).toHaveLength(3)
    // The new camera starts with its best layer only, until the server says.
    expect(encodingOf(cameraLow1).active).toBe(false)
  })

  it('keeps going when a lower line refuses the track or the capture cannot be read', async () => {
    const { rtc, chat, voice, camera, cameraLow1, cameraLow2 } = await joinedWithCameraLayers()
    const { stream, track } = cameraCapture(1280, 720)
    cameraFrom(stream)
    // The browser cannot put the capture on the first lower line.
    cameraLow1.replaceTrack.mockRejectedValueOnce(new Error('InvalidModificationError'))
    await rtc.startCamera()
    expect(cameraLow1.track).toBeNull()
    expect(cameraLow2.track).toBe(track)
    expect(camera.track).toBe(track)

    // A capture that cannot be read once does not stop later changes.
    const settings = track.getSettings
    track.getSettings = () => { throw new Error('ended') }
    chat.handleWSEvent({ type: 'screen_layers_wanted', payload: { channel_id: 'ch-1', layers: [0, 1], source: 'camera' } })
    await vi.waitFor(() => expect(voice.cameraLayersWanted).toEqual([0, 1]))
    track.getSettings = settings
    chat.handleWSEvent({ type: 'screen_layers_wanted', payload: { channel_id: 'ch-1', layers: [0, 2], source: 'camera' } })
    await vi.waitFor(() => expect(encodingOf(cameraLow2).active).toBe(true))

    // Taking a track off a line that refuses is no problem either.
    cameraLow2.replaceTrack.mockRejectedValueOnce(new Error('closed'))
    rtc.stopCamera()
    await vi.waitFor(() => expect(cameraLow2.replaceTrack).toHaveBeenCalledWith(null))
  })

  it('declares a running camera again before it goes out on a new connection', async () => {
    const { rtc, voice, chat, sent } = await joinedWithCameraLayers()
    const { stream, track } = cameraCapture(1280, 720)
    cameraFrom(stream)
    await rtc.startCamera()
    expect(cameraDeclarations(sent)).toHaveLength(1)

    voice.serverCameraLayers = 3
    rtc.rejoinAfterReconnect()
    const pc = present(FakePC.instances.at(-1))
    // The new connection's lines are bound from its offer: the camera goes out at once, so the server must already know its layers.
    const known: number[] = []
    const camera = () => pc.senders[2]
    pc.setRemoteDescription = new Proxy(pc.setRemoteDescription.bind(pc), {
      apply: async (target, self, args) => {
        await Reflect.apply(target, self, args)
        const sender = camera()
        if (sender) {
          const attach = sender.replaceTrack.getMockImplementation()
          sender.replaceTrack.mockImplementation(async (t) => { known.push(cameraDeclarations(sent).length); await attach?.(t) })
        }
      }
    })
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_CAMERA_LAYER_OFFER } })
    await vi.waitFor(() => expect(pc.transceivers.length).toBe(8))
    await vi.waitFor(() => expect(present(pc.senders[6]).track).toBe(track))
    await vi.waitFor(() => expect(present(pc.senders[7]).track).toBe(track))
    expect(present(pc.senders[2]).track).toBe(track)
    expect(known).toEqual([2])
    expect(cameraDeclarations(sent)).toHaveLength(2)
    expect(encodingOf(present(pc.senders[6]))).toMatchObject({ active: false, scaleResolutionDownBy: 2 })
  })
})
