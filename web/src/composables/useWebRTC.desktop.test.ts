import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWebRTC } from './useWebRTC'
import { useVoiceStore } from '../stores/voice'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { save as saveVoiceSession } from '../lib/voiceSession'
import { t } from '../i18n'

const capture = vi.fn(), display = vi.fn(), network = vi.fn(), peer = vi.fn()
const channelId = '00000000-0000-4000-8000-000000000001'
beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  vi.stubGlobal('isTauri', true)
  vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: capture, getDisplayMedia: display } })
  vi.stubGlobal('fetch', network)
  vi.stubGlobal('RTCPeerConnection', peer)
})
afterEach(() => { vi.unstubAllGlobals(); localStorage.clear() })

it.each(['voice', 'camera', 'screen'] as const)('rejects unqualified desktop %s before physical capture, signaling or connection state', async action => {
  const rtc = useWebRTC()
  const chat = useChatStore()
  const send = vi.spyOn(chat, 'sendWSEvent')
  const error = vi.spyOn(useToastStore(), 'error')
  if (action === 'voice') await rtc.joinVoiceChannel(channelId)
  else if (action === 'camera') await rtc.startCamera()
  else await rtc.startScreenShare()
  expect(error).toHaveBeenCalledExactlyOnceWith(t('nativeDesktop.mediaUnavailable'))
  expect(capture).not.toHaveBeenCalled()
  expect(display).not.toHaveBeenCalled()
  expect(network).not.toHaveBeenCalled()
  expect(peer).not.toHaveBeenCalled()
  expect(send).not.toHaveBeenCalled()
  expect(useVoiceStore().currentChannelId).toBeNull()
  expect(useVoiceStore().isConnected).toBe(false)
})

it('does not revive a saved browser call or rejoin through the native metadata socket', async () => {
  saveVoiceSession(channelId)
  const rtc = useWebRTC()
  const send = vi.spyOn(useChatStore(), 'sendWSEvent')
  expect(await rtc.resumeVoiceSession()).toBe(false)
  expect(useVoiceStore().currentChannelId).toBeNull()
  useVoiceStore().currentChannelId = channelId
  rtc.rejoinAfterReconnect()
  expect(capture).not.toHaveBeenCalled()
  expect(display).not.toHaveBeenCalled()
  expect(network).not.toHaveBeenCalled()
  expect(peer).not.toHaveBeenCalled()
  expect(send).not.toHaveBeenCalled()
})
