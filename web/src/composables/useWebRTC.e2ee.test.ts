import { describe, it, expect, vi } from 'vitest'
import { present } from '../media-test.fixture'
import { fixtureId, userFixture, voiceUserFixture } from '../test-fixtures.fixture'
import { useAuthStore } from '../stores/auth'
import { nextTick } from 'vue'
import { t } from '../i18n'
import { useVoiceStore } from '../stores/voice'
import { useToastStore } from '../stores/toast'
import { installHarness, FakePC, audioElements, fakeTrack, grantMic, installRecordingCall, micRequests, negotiate, remoteStreamFor, setup } from './useWebRTC.harness.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

describe('protected media', () => {
  it('hands the call the members the Talk already lists as soon as it starts', async () => {
    const call = installRecordingCall()
    const user = userFixture()
    const { rtc, voice } = setup()
    useAuthStore().user = user
    const reloading = voiceUserFixture({ id: fixtureId(3) })
    voice.channelUsers = { 'ch-1': { [reloading.id]: reloading } }
    const join = rtc.joinVoiceChannel('ch-1')
    expect(call.start).toHaveBeenCalledWith('ch-1', user.id)
    expect(call.roster).toHaveBeenCalledWith([reloading.id])
    expect(call.start.mock.invocationCallOrder[0]).toBeLessThan(call.roster.mock.invocationCallOrder[0] ?? 0)
    await grantMic()
    await join
  })

  it('tells the call what each incoming line carries, from the stream the SFU names', async () => {
    const call = installRecordingCall()
    const { rtc } = setup()
    useAuthStore().user = userFixture()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = await negotiate()
    const ontrack = present(pc.ontrack) as unknown as (event: unknown) => void
    const receiver = (name: string) => ({ name }) as unknown as RTCRtpReceiver
    ontrack({ track: fakeTrack('audio'), streams: [remoteStreamFor('alice')], receiver: receiver('mic') })
    ontrack({ track: fakeTrack('audio'), streams: [remoteStreamFor('screen:alice')], receiver: receiver('screenAudio') })
    ontrack({ track: fakeTrack('video'), streams: [remoteStreamFor('alice')], receiver: receiver('screen') })
    ontrack({ track: fakeTrack('video'), streams: [remoteStreamFor('cam:alice')], receiver: receiver('camera') })
    expect(call.protectIncoming.mock.calls.map(c => [(c[0] as unknown as { name: string }).name, c[1], c[2]])).toEqual([
      ['mic', 'alice', 0], ['screenAudio', 'alice', 3], ['screen', 'alice', 2], ['camera', 'alice', 1],
    ])
  })

  it('seals the call for the signed-in member and ignores another room', async () => {
    const call = installRecordingCall()
    const user = userFixture()
    const { rtc, chat, voice, sent } = setup()
    useAuthStore().user = user
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    expect(call.start).toHaveBeenCalledWith('ch-1', user.id)
    const member = voiceUserFixture({ id: fixtureId(3) })
    voice.channelUsers = { 'ch-1': { [member.id]: member } }
    await nextTick()
    expect(call.roster).toHaveBeenLastCalledWith([member.id])
    const pc = await negotiate()
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('alice')] })
    expect(call.protectIncoming).toHaveBeenCalledWith(undefined, 'alice', 0)
    expect(audioElements.length).toBeGreaterThan(0)
    const keyframes = sent.filter(event => event.type === 'webrtc_request_keyframe').length
    call.fireKey()
    expect(sent.filter(event => event.type === 'webrtc_request_keyframe')).toHaveLength(keyframes + 1)
    call.fireWire('ch-1', 'abc')
    call.fireWire('other-room', 'nope')
    expect(sent.filter(event => event.type === 'media_handshake')).toEqual([
      { type: 'media_handshake', payload: { channel_id: 'ch-1', wire: 'abc' } },
    ])
    const sender = fixtureId(2)
    chat.handleWSEvent({ type: 'media_handshake', payload: { channel_id: 'ch-1', user_id: sender, wire: 'abc' } })
    chat.handleWSEvent({ type: 'media_handshake', payload: { channel_id: 'other-room', user_id: sender, wire: 'nope' } })
    expect(call.receive).toHaveBeenCalledTimes(1)
    expect(call.receive).toHaveBeenCalledWith(sender, 'abc')
    rtc.leaveVoiceChannel()
    expect(call.stop).toHaveBeenCalledOnce()
    call.fireKey()
    expect(sent.filter(event => event.type === 'webrtc_request_keyframe')).toHaveLength(keyframes + 1)
    expect(useToastStore().toasts).toEqual([])
  })

  it('keeps the safety code of the current keys in the voice store', async () => {
    const call = installRecordingCall()
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const verification = { epoch: 3, code: '01234 56789 01234 56789', members: [fixtureId(1)] }
    call.fireVerification(verification)
    expect(useVoiceStore().mediaVerification).toEqual(verification)
    call.fireVerification(null)
    expect(useVoiceStore().mediaVerification).toBeNull()
  })

  it('tells the member once per call that the group removed them', async () => {
    const call = installRecordingCall()
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    call.fireNotice()
    call.fireNotice()
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual([t('voice.mediaRekeyed')])
    rtc.leaveVoiceChannel()
    const again = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await again
    call.fireNotice()
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual([t('voice.mediaRekeyed'), t('voice.mediaRekeyed')])
  })

  it('says the call was refreshed, not reset, when the member was dropped after its own inactivity', async () => {
    const call = installRecordingCall()
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    call.fireNotice('absent')
    call.fireNotice('removed')
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual([t('voice.mediaRefreshed')])
    expect(t('voice.mediaRefreshed')).not.toBe(t('voice.mediaRekeyed'))
  })

  it('tells the member once per call that someone uses an outdated client, apart from the rekey notices', async () => {
    const call = installRecordingCall()
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    call.fireNotice('outdated')
    call.fireNotice('outdated')
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual([t('voice.mediaOutdatedClient')])
    call.fireNotice('removed')
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual([t('voice.mediaOutdatedClient'), t('voice.mediaRekeyed')])
  })

  it('arms every receiver before it answers the offer', async () => {
    const call = installRecordingCall()
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const order: string[] = []
    call.prepareIncoming.mockImplementation(() => { order.push('prepare'); return true })
    vi.spyOn(FakePC.prototype, 'setLocalDescription').mockImplementation(async () => { order.push('answer') })
    const pc = await negotiate()
    expect(call.prepareIncoming.mock.calls.map(([receiver]) => receiver)).toEqual(pc.transceivers.map(t => t.receiver))
    expect(order).toEqual(['prepare', 'prepare', 'prepare', 'prepare', 'answer'])
  })

  it('holds a handshake until the voice join is sent', async () => {
    const call = installRecordingCall()
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(micRequests.length).toBeGreaterThan(0))
    call.fireWire('ch-1', 'early')
    call.fireWire('other-room', 'nope')
    expect(sent.some(event => event.type === 'media_handshake')).toBe(false)
    await grantMic()
    await join
    const joinAt = sent.findIndex(event => event.type === 'voice_join')
    const handshakeAt = sent.findIndex(event => event.type === 'media_handshake')
    expect(handshakeAt).toBeGreaterThan(joinAt)
    call.fireWire('ch-1', 'later')
    expect(sent.filter(event => event.type === 'media_handshake')).toEqual([
      { type: 'media_handshake', payload: { channel_id: 'ch-1', wire: 'early' } },
      { type: 'media_handshake', payload: { channel_id: 'ch-1', wire: 'later' } },
    ])
  })

  it('drops a held handshake when the member has left the room', async () => {
    const call = installRecordingCall()
    const { rtc, sent, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(micRequests.length).toBeGreaterThan(0))
    call.fireWire('ch-1', 'early')
    voice.currentChannelId = 'ch-2'
    await grantMic()
    await join
    expect(sent.filter(event => event.type === 'media_handshake')).toEqual([])
    rtc.leaveVoiceChannel()
    call.fireWire('ch-2', 'after')
    expect(sent.filter(event => event.type === 'media_handshake')).toEqual([])
  })

  it('does not publish or play when the call cannot be protected', async () => {
    const call = installRecordingCall(false)
    const { rtc, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = await negotiate()
    for (const sender of pc.senders) expect(sender.replaceTrack).not.toHaveBeenCalled()
    present(pc.ontrack)({ track: fakeTrack(), streams: [remoteStreamFor('alice')] })
    present(pc.ontrack)({ track: fakeTrack('video'), streams: [remoteStreamFor('bob')] })
    expect(audioElements).toEqual([])
    expect(voice.remoteScreenStream).toBeNull()
    expect(voice.userVideoStreams).toEqual({})
    expect(call.protectSender).toHaveBeenCalled()
    expect(call.protectIncoming).toHaveBeenCalled()
    expect(voice.localAudioStream).toBeNull()
    expect(voice.localCameraStream).toBeNull()
    expect(voice.localScreenStream).toBeNull()
    const captures = vi.mocked(navigator.mediaDevices.getUserMedia).mock.calls.length
    await rtc.startCamera()
    await rtc.startScreenShare()
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledTimes(captures)
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual([t('voice.mediaUnprotected')])
  })

  it('stops a camera prompt that resolves after the call is unprotected', async () => {
    installRecordingCall(false)
    const { rtc, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(micRequests.length).toBe(1))
    const pendingCamera = rtc.startCamera()
    await vi.waitFor(() => expect(micRequests.length).toBe(2))
    present(micRequests.shift())()
    await join
    await negotiate()
    const camera = present(micRequests.shift())
    const track = present(camera.stream.getTracks()[0])
    camera()
    await pendingCamera
    expect(voice.isCameraOn).toBe(false)
    expect(voice.localCameraStream).toBeNull()
    expect(track.stop).toHaveBeenCalled()
    expect(useToastStore().toasts.map(toast => toast.text)).toEqual([t('voice.mediaUnprotected')])
  })

  it('sends the handshake again when the call reconnects', async () => {
    const call = installRecordingCall()
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    rtc.rejoinAfterReconnect()
    expect(call.resume).toHaveBeenCalledOnce()
  })
})
