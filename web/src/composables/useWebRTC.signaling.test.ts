import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { present } from '../media-test.fixture'
import { decodeServerEvent } from '../types/events'
import { fixtureId } from '../test-fixtures.fixture'
import { useWebRTC } from './useWebRTC'
import { useChatStore } from '../stores/chat'
import { installHarness, permissiveMediaCall, type ClientSentEvent, FakeAudioContext, FakePC, SFU_OFFER, fakeStream, fakeTrack, grantMic, joined, micRequests, negotiate, remoteStreamFor, setup } from './useWebRTC.harness.fixture'

vi.mock('../lib/noiseSuppressor', async () => (await import('./useWebRTC.noiseSuppressor.fixture')).noiseSuppressorModule)
vi.mock('../lib/soundEffects', () => ({ playSoundEffect: vi.fn() }))

installHarness()

describe('useWebRTC join and leave', () => {
  it('joins with the microphone attached', async () => {
    const { rtc, sent, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    expect(sent.filter(e => e.type === 'voice_join')).toEqual([{ type: 'voice_join', payload: { channel_id: 'ch-1', tab: expect.any(String) } }])
    expect(voice.currentChannelId).toBe('ch-1')
    // We send on the SFU's own mic, screen and camera lines (never on lines
    // added locally, which a browser would not match to the offer); only
    // the mic carries a track yet.
    const pc = await negotiate()
    expect(pc.unmatched).toHaveLength(0)
    expect(pc.transceivers.map(t => t.direction)).toEqual(['sendrecv', 'sendrecv', 'sendrecv', 'sendrecv'])
    const senders = pc.senders
    expect(senders).toHaveLength(4)
    expect(senders.filter(sd => sd.track)).toHaveLength(1)
  })

  it('leaving while the mic prompt is open leaves no ghost and stops the mic', async () => {
    const { rtc, sent, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(micRequests.length).toBe(1))
    rtc.leaveVoiceChannel()
    const stream = await grantMic()
    await join
    expect(sent.some(e => e.type === 'voice_join')).toBe(false)
    expect(present(stream.getTracks()[0]).stop).toHaveBeenCalled()
    expect(voice.currentChannelId).toBeNull()
  })

  it('does not hang on an audio context that waits for a user gesture', async () => {
    class GestureLockedContext extends FakeAudioContext {
      constructor() { super(); this.state = 'suspended' }
      resume() { return new Promise<void>(() => {}) }
    }
    vi.stubGlobal('AudioContext', GestureLockedContext)
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    expect(sent.some(e => e.type === 'voice_join')).toBe(true)
  })

  it('a second join supersedes a pending first one', async () => {
    const { rtc, sent } = setup()
    const first = rtc.joinVoiceChannel('ch-1')
    await vi.waitFor(() => expect(micRequests.length).toBe(1))
    const second = rtc.joinVoiceChannel('ch-2')
    const firstStream = await grantMic()
    await grantMic()
    await Promise.all([first, second])
    const joins = sent.filter(e => e.type === 'voice_join').map(e => ('channel_id' in e.payload ? e.payload.channel_id : undefined))
    expect(joins).toEqual(['ch-2'])
    expect(present(firstStream.getTracks()[0]).stop).toHaveBeenCalled()
  })
})

describe('shared watchers', () => {
  it('reacts once to a setting however many components use it', async () => {
    const { rtc, sent, voice } = setup()
    useWebRTC()
    useWebRTC()
    useWebRTC()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const before = sent.filter(e => e.type === 'voice_mute_state').length
    voice.isMuted = true
    await vi.waitFor(() => expect(sent.filter(e => e.type === 'voice_mute_state').length).toBe(before + 1))
    await new Promise(r => setTimeout(r, 0))
    expect(sent.filter(e => e.type === 'voice_mute_state').length).toBe(before + 1)
  })
})

describe('kicked from voice', () => {
  it('ends the call locally without a leave and does not rejoin', async () => {
    const { rtc, chat, voice, sent } = setup()
    const { useToastStore } = await import('../stores/toast')
    const { t } = await import('../i18n')
    const { recent } = await import('../lib/voiceSession')
    const join = rtc.joinVoiceChannel('ch-1')
    const mic = await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))

    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1' } })
    expect(voice.currentChannelId).toBeNull()
    expect(pc.closed).toBe(true)
    expect(present(mic.getTracks()[0]).stop).toHaveBeenCalled()
    expect(sent.some(e => e.type === 'voice_leave')).toBe(false)
    expect(recent()).toBeNull()
    expect(useToastStore().toasts.map(x => x.text)).toContain(t('voice.kicked'))

    const joins = sent.filter(e => e.type === 'voice_join').length
    rtc.rejoinAfterReconnect()
    expect(sent.filter(e => e.type === 'voice_join').length).toBe(joins)
  })

  it.each(['de', 'en'] as const)('ends a refused full-room join with the %s capacity message and no retry', async language => {
    const { rtc, chat, voice, sent } = setup()
    const { useToastStore } = await import('../stores/toast')
    const { setLocale, t } = await import('../i18n')
    const { recent } = await import('../lib/voiceSession')
    setLocale(language)
    try {
      const channelId = fixtureId(301)
      const join = rtc.joinVoiceChannel(channelId)
      const mic = await grantMic()
      await join
      const pc = present(FakePC.instances.at(-1))
      const joins = sent.filter(event => event.type === 'voice_join').length
      const refusal = decodeServerEvent({ type: 'voice_kicked', payload: { channel_id: channelId, reason: 'room_full' } })
      expect(refusal).toEqual({ type: 'voice_kicked', payload: { channel_id: channelId, reason: 'room_full' } })
      chat.handleWSEvent(present(refusal))
      expect(voice.currentChannelId).toBeNull()
      expect(voice.isConnected).toBe(false)
      expect(pc.closed).toBe(true)
      expect(present(mic.getTracks()[0]).stop).toHaveBeenCalled()
      expect(recent()).toBeNull()
      expect(sent.some(event => event.type === 'voice_leave')).toBe(false)
      expect(useToastStore().toasts.map(toast => toast.text)).toContain(t('voice.roomFull'))
      expect(useToastStore().toasts.map(toast => toast.text)).not.toContain(t('voice.kicked'))
      expect(t('voice.roomFull')).toBe(language === 'de'
        ? 'Dieser Talk ist voll. Versuche es erneut, sobald jemand den Talk verlässt.'
        : 'This Talk is full. Try again when someone leaves.')
      rtc.rejoinAfterReconnect()
      expect(sent.filter(event => event.type === 'voice_join')).toHaveLength(joins)
    } finally { setLocale('de') }
  })

  it('refuses to start a join the member may not make', async () => {
    const { rtc, sent, voice } = setup()
    const { useToastStore } = await import('../stores/toast')
    const { usePermissionsStore } = await import('../stores/permissions')
    const { t } = await import('../i18n')
    vi.spyOn(usePermissionsStore(), 'canIn').mockReturnValue(false)
    await rtc.joinVoiceChannel('ch-1')
    expect(voice.currentChannelId).toBeNull()
    expect(sent.some(event => event.type === 'voice_join')).toBe(false)
    expect(useToastStore().toasts.map(toast => toast.text)).toContain(t('talk.noConnect'))
  })

  it('refuses to join with a browser that cannot seal call media, and asks for no microphone', async () => {
    const { rtc, sent, voice } = setup()
    const { useToastStore } = await import('../stores/toast')
    const { installMediaCall } = await import('../lib/media/api')
    const { t } = await import('../i18n')
    installMediaCall({ ...permissiveMediaCall, canSeal: () => Promise.resolve(false) })
    try {
      await rtc.joinVoiceChannel('ch-1')
    } finally { installMediaCall(permissiveMediaCall) }
    expect(voice.currentChannelId).toBeNull()
    expect(micRequests).toHaveLength(0)
    expect(sent.some(event => event.type === 'voice_join')).toBe(false)
    expect(useToastStore().toasts.map(toast => toast.text)).toContain(t('voice.mediaUnsupported'))
  })

  it('ends a call whose connect permission was withdrawn', async () => {
    const { rtc, chat, voice } = setup()
    const { useToastStore } = await import('../stores/toast')
    const { t } = await import('../i18n')
    const join = rtc.joinVoiceChannel('ch-2')
    await grantMic()
    await join
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-2', reason: 'missing_permission' } })
    expect(voice.currentChannelId).toBeNull()
    expect(useToastStore().toasts.map(toast => toast.text)).toContain(t('voice.noPermission'))
  })

  it('ignores a stale full-room refusal for a different channel', async () => {
    const { rtc, chat, voice } = setup()
    const { useToastStore } = await import('../stores/toast')
    const join = rtc.joinVoiceChannel('ch-2')
    await grantMic()
    await join
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1', reason: 'room_full' } })
    expect(voice.currentChannelId).toBe('ch-2')
    expect(present(FakePC.instances.at(-1)).closed).toBe(false)
    expect(useToastStore().toasts).toEqual([])
  })

  it('ignores a kick from a channel I already left', async () => {
    const { rtc, chat, voice } = setup()
    const join = rtc.joinVoiceChannel('ch-2')
    await grantMic()
    await join
    chat.handleWSEvent({ type: 'voice_kicked', payload: { channel_id: 'ch-1' } })
    expect(voice.currentChannelId).toBe('ch-2')
  })
})

describe('useWebRTC signaling', () => {
  it('discards an offer suspended on a connection replaced by reconnect', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const old = present(FakePC.instances.at(-1))
    let release: (() => void) | undefined
    const original = old.setRemoteDescription.bind(old)
    old.setRemoteDescription = vi.fn(async description => {
      await new Promise<void>(resolve => { release = resolve })
      await original(description)
    })
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(release).toBeTypeOf('function'))
    rtc.rejoinAfterReconnect()
    const replacement = present(FakePC.instances.at(-1))
    present(release)()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(replacement.transceivers).toEqual([])
    expect(sent.filter(event => event.type === 'webrtc_answer')).toEqual([])
    await negotiate()
    expect(sent.filter(event => event.type === 'webrtc_answer')).toHaveLength(1)
  })

  it('does not publish an old answer after leaving during answer creation', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const old = present(FakePC.instances.at(-1))
    let release: ((value: RTCSessionDescriptionInit) => void) | undefined
    old.createAnswer = vi.fn(() => new Promise<RTCSessionDescriptionInit>(resolve => { release = resolve }))
    old.setLocalDescription = vi.fn(async () => {})
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(release).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(release)({ type: 'answer', sdp: 'old-session' })
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(old.setLocalDescription).not.toHaveBeenCalled()
    expect(sent.filter(event => event.type === 'webrtc_answer')).toEqual([])
  })

  it('ignores ICE and media callbacks from a retired connection', async () => {
    const { rtc, voice, sent } = await joined()
    const old = present(FakePC.instances.at(-1))
    rtc.rejoinAfterReconnect()
    const before = sent.length
    present(old.onicecandidate)({ candidate: { toJSON: () => ({ candidate: 'retired' }) } })
    present(old.ontrack)({ track: fakeTrack('video'), streams: [remoteStreamFor('cam:retired')] })
    expect(sent).toHaveLength(before)
    expect(voice.userVideoStreams).toEqual({})
  })

  it('configures screen limits before restoring a share on a negotiated new sender', async () => {
    const { rtc } = await joined()
    const stream = fakeStream(['video'])
    present(stream.getVideoTracks()[0]).getSettings = () => ({ width: 1920, height: 1080 })
    navigator.mediaDevices.getDisplayMedia = vi.fn().mockResolvedValue(stream)
    await rtc.startScreenShare()
    rtc.rejoinAfterReconnect()
    const replacement = await negotiate()
    const screen = present(replacement.senders[1])
    expect(screen.track).toBe(present(stream.getVideoTracks()[0]))
    expect(present(screen.setParameters.mock.invocationCallOrder[0])).toBeLessThan(present(screen.replaceTrack.mock.invocationCallOrder[0]))
  })

  it('ignores offers that arrive after leaving', async () => {
    const { rtc, chat } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    rtc.leaveVoiceChannel()
    const before = FakePC.instances.length
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await new Promise(r => setTimeout(r, 10))
    expect(FakePC.instances.length).toBe(before)
  })

  it('buffers ICE candidates until the offer is applied, then answers', async () => {
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const chat = useChatStore()
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'c1' } })
    await new Promise(r => setTimeout(r, 10))
    const pc = present(FakePC.instances.at(-1))
    expect(pc.candidates).toEqual([])
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(sent.some(e => e.type === 'webrtc_answer')).toBe(true))
    expect(pc.candidates).toEqual([{ candidate: 'c1' }])
  })

  it('applies the offer with H.264 first on the screen line when the browser can send it', async () => {
    vi.stubGlobal('RTCRtpSender', { getCapabilities: () => ({ codecs: [{ mimeType: 'video/VP8' }, { mimeType: 'video/H264' }] }) })
    const { rtc } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    const offer = SFU_OFFER.replace(/(m=video 9 UDP\/TLS\/RTP\/SAVPF) 96(\r\na=mid:(\d))/g, (_, m, rest) =>
      `${m} 96 102${rest}\r\na=rtpmap:96 VP8/90000\r\na=rtpmap:102 H264/90000\r\na=fmtp:102 packetization-mode=1;profile-level-id=42e01f`)
    useChatStore().handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: offer } })
    await vi.waitFor(() => expect(present(pc.remoteDescription)).not.toBeNull())
    const applied = present(pc.remoteDescription).sdp
    expect(present(pc.remoteDescription).type).toBe('offer')
    expect(applied).toContain('m=video 9 UDP/TLS/RTP/SAVPF 102 96\r\na=mid:1')
    expect(applied).toContain('m=video 9 UDP/TLS/RTP/SAVPF 96 102\r\na=mid:2')
    expect(applied).toContain('x-google-start-bitrate')
  })

  it('reconnect starts a fresh connection and announces the join again', async () => {
    const { rtc, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const oldPC = present(FakePC.instances.at(-1))
    rtc.rejoinAfterReconnect()
    expect(oldPC.closed).toBe(true)
    expect(present(FakePC.instances.at(-1))).not.toBe(oldPC)
    expect(sent.filter(e => e.type === 'voice_join')).toHaveLength(2)
  })
})

describe('video subscriptions', () => {
  const subs = (sent: ClientSentEvent[]) => sent.filter(e => e.type === 'webrtc_subscribe').map(e => e.payload)

  beforeEach(() => localStorage.clear())

  it('sends camera opt-outs right after joining, nothing by default', async () => {
    const first = await joined()
    expect(subs(first.sent)).toEqual([])
    first.rtc.leaveVoiceChannel()

    localStorage.setItem('mnema_hidden_cameras', JSON.stringify({ alice: true }))
    setActivePinia(createPinia())
    const { sent } = await joined()
    const types = sent.map(e => e.type)
    expect(types.indexOf('webrtc_subscribe')).toBeGreaterThan(types.indexOf('voice_join'))
    expect(subs(sent)).toEqual([{ kind: 'camera', user_id: 'alice', on: false }])
  })

  it('forwards later changes: hide camera, opt into and out of a screen share', async () => {
    const { voice, sent } = await joined()
    voice.setCameraHidden('alice', true)
    voice.watchScreen('bob')
    voice.unwatchScreen('bob')
    voice.setAllCamerasOff(true)
    expect(subs(sent)).toEqual([
      { kind: 'camera', user_id: 'alice', on: false },
      { kind: 'screen', user_id: 'bob', on: true },
      { kind: 'screen', user_id: 'bob', on: false },
      { kind: 'camera', all: true, on: false }
    ])
  })

  it('resends opt-outs and still-running screen opt-ins after a reconnect', async () => {
    const { rtc, voice, sent } = await joined()
    voice.setCameraHidden('alice', true)
    voice.handleMediaState({ user_id: 'bob', screen: true })
    voice.handleMediaState({ user_id: 'carol', screen: true })
    voice.watchScreen('bob')
    voice.watchScreen('carol')
    // carol's share ended while the connection was down
    voice.mediaState = { bob: { channel_id: 'ch-1', screen: true, camera: false } }
    sent.length = 0
    rtc.rejoinAfterReconnect()
    expect(subs(sent)).toEqual([
      { kind: 'camera', user_id: 'alice', on: false },
      { kind: 'screen', user_id: 'bob', on: true }
    ])
    expect(voice.watchedScreens).toEqual({ bob: true })
  })

  it('stops sending after leaving', async () => {
    const { rtc, voice, sent } = await joined()
    rtc.leaveVoiceChannel()
    sent.length = 0
    voice.setCameraHidden('alice', true)
    expect(subs(sent)).toEqual([])
  })

  it('applies webrtc_media_state events', async () => {
    const { chat, voice } = await joined()
    chat.handleWSEvent({ type: 'webrtc_media_state', payload: { channel_id: 'ch-1', user_id: 'bob', screen: true, camera: false } })
    expect(voice.mediaState.bob).toEqual({ channel_id: 'ch-1', screen: true, camera: false })
  })
})

describe('negotiation retry, ordering and sender capabilities', () => {
  it.each(['setRemoteDescription', 'createAnswer', 'setLocalDescription'] as const)('reports %s failures and accepts the next offer', async method => {
    const { chat, pc, sent } = await joined()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fail = vi.spyOn(pc, method).mockRejectedValueOnce(new Error('rejected SDP'))
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(sent).toContainEqual({ type: 'webrtc_diag', payload: { event: 'negotiation_error', error: 'rejected SDP' } }))
    fail.mockRestore()
    const answers = sent.filter(e => e.type === 'webrtc_answer').length
    await negotiate()
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(answers + 1)
  })

  it('reports non-Error rejection values without blocking later offers', async () => {
    const { chat, pc, sent } = await joined()
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(pc, 'setRemoteDescription').mockRejectedValueOnce('invalid')
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: '' } })
    await vi.waitFor(() => expect(sent).toContainEqual({ type: 'webrtc_diag', payload: { event: 'negotiation_error', error: 'invalid' } }))
  })

  it('drops ICE after leaving and handles rejected live candidates while preserving later candidates', async () => {
    const { rtc, chat, pc } = await joined()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    vi.spyOn(pc, 'addIceCandidate').mockRejectedValueOnce(new Error('bad candidate'))
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'bad' } })
    await vi.waitFor(() => expect(warn).toHaveBeenCalled())
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'good' } })
    await vi.waitFor(() => expect(pc.candidates).toEqual([{ candidate: 'good' }]))
    rtc.leaveVoiceChannel()
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'late' } })
    expect(pc.candidates).toEqual([{ candidate: 'good' }])
  })

  it('suppresses rejected ICE warnings from a replaced connection', async () => {
    const { rtc, chat, pc } = await joined()
    let fail: ((reason: Error) => void) | undefined
    pc.addIceCandidate = vi.fn(() => new Promise<void>((_resolve, reject) => { fail = reject }))
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'pending' } })
    rtc.rejoinAfterReconnect()
    present(fail)(new Error('old connection'))
    await Promise.resolve()
    expect(warn).not.toHaveBeenCalled()
  })

  it('answers despite a rejected queued ICE candidate', async () => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    vi.spyOn(pc, 'addIceCandidate').mockRejectedValueOnce(new Error('candidate'))
    chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'early' } })
    await negotiate()
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(1)
  })

  it.each(['candidate', 'local', 'tuning'] as const)('drops a retired offer after its %s await', async stage => {
    const { rtc, chat, sent } = setup()
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    let finish: (() => void) | undefined
    const defer = () => new Promise<void>(resolve => { finish = resolve })
    if (stage === 'candidate') {
      chat.handleWSEvent({ type: 'webrtc_candidate', payload: { candidate: 'early' } })
      pc.addIceCandidate = vi.fn(defer)
    } else if (stage === 'local') pc.setLocalDescription = vi.fn(defer)
    else {
      const original = pc.setRemoteDescription.bind(pc)
      pc.setRemoteDescription = async description => {
        await original(description)
        present(pc.senders[0]).setParameters = vi.fn(defer)
      }
    }
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(finish)()
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(stage === 'tuning' ? 1 : 0)
    expect(sent.filter(e => e.type === 'webrtc_diag' && 'event' in e.payload && e.payload.event === 'negotiation_error')).toEqual([])
  })

  it('ignores a rejected obsolete offer instead of publishing its diagnostics', async () => {
    const { rtc, chat, pc, sent } = await joined()
    let fail: ((reason: Error) => void) | undefined
    pc.setRemoteDescription = vi.fn(() => new Promise<void>((_resolve, reject) => { fail = reject }))
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(fail).toBeTypeOf('function'))
    rtc.leaveVoiceChannel()
    present(fail)(new Error('obsolete'))
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(sent.filter(e => e.type === 'webrtc_diag' && 'event' in e.payload && e.payload.event === 'negotiation_error')).toEqual([])
  })

  it('negotiates offers without publish lines and with a partial mic line', async () => {
    const { chat, pc, sent } = await joined()
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: 'v=0\r\n' } })
    await vi.waitFor(() => expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(2))
    expect(pc.unmatched).toEqual([])
  })

  it('tolerates browsers rejecting capability lookup and tuning fields', async () => {
    vi.stubGlobal('RTCRtpSender', { getCapabilities: () => { throw new Error('unsupported') } })
    const debug = vi.spyOn(console, 'debug').mockImplementation(() => {})
    const { rtc, chat, voice, sent } = setup()
    voice.qosHighPriority = false
    const join = rtc.joinVoiceChannel('ch-1')
    await grantMic()
    await join
    const pc = present(FakePC.instances.at(-1))
    const original = pc.setRemoteDescription.bind(pc)
    pc.setRemoteDescription = async description => {
      await original(description)
      for (const sender of pc.senders) {
        sender.getParameters = () => ({ transactionId: '', codecs: [], headerExtensions: [], rtcp: {}, encodings: [] })
        sender.setParameters.mockRejectedValue(new Error('unknown field'))
      }
    }
    chat.handleWSEvent({ type: 'webrtc_offer', payload: { type: 'offer', sdp: SFU_OFFER } })
    await vi.waitFor(() => expect(debug).toHaveBeenCalledTimes(4))
    expect(sent.filter(e => e.type === 'webrtc_answer')).toHaveLength(1)
    expect(present(present(pc.senders[0]).setParameters.mock.calls[0])?.[0]?.encodings[0]).toMatchObject({ priority: 'medium', networkPriority: 'medium' })
  })
})

describe('server ICE configuration initialization', () => {
  it('retries a failed config request on a later join and applies only validated servers', async () => {
    vi.resetModules()
    const isolated = (await import('./useWebRTC')).useWebRTC()
    // The real call cannot seal in jsdom (no frame transform) and would refuse the join.
    ;(await import('../lib/media/api')).installMediaCall(permissiveMediaCall)
    const chat = useChatStore()
    chat.sendWSEvent = vi.fn()
    const fetch = vi.mocked(globalThis.fetch)
    fetch.mockRejectedValueOnce(new Error('offline'))
    const first = isolated.joinVoiceChannel('first')
    await grantMic()
    await first
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(present(FakePC.instances.at(-1)).configuration.iceServers).toEqual([])
    isolated.leaveVoiceChannel()
    fetch.mockResolvedValueOnce(new Response(JSON.stringify({ ice_servers: [{ urls: ['stun:stun.example.invalid'] }] }), { status: 200, headers: { 'content-type': 'application/json' } }))
    const second = isolated.joinVoiceChannel('second')
    await grantMic()
    await second
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(present(FakePC.instances.at(-1)).configuration.iceServers).toEqual([{ urls: ['stun:stun.example.invalid'] }])
    isolated.leaveVoiceChannel()
  })

  it('handles an HTTP config failure without preventing a local voice connection', async () => {
    vi.resetModules()
    const isolated = (await import('./useWebRTC')).useWebRTC()
    // The real call cannot seal in jsdom (no frame transform) and would refuse the join.
    ;(await import('../lib/media/api')).installMediaCall(permissiveMediaCall)
    useChatStore().sendWSEvent = vi.fn()
    vi.mocked(globalThis.fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    const join = isolated.joinVoiceChannel('local-only')
    await grantMic()
    await join
    expect(present(FakePC.instances.at(-1)).configuration.iceServers).toEqual([])
    isolated.leaveVoiceChannel()
  })
})

