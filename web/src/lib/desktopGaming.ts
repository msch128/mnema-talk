import { watch } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useVoiceStore } from '../stores/voice'
import { useWebRTC, setGamingPttLease } from '../composables/useWebRTC'

export interface GamingChord { key: number; alt: boolean; control: boolean; shift: boolean }
export interface GamingSettings { overlay: GamingChord; mute: GamingChord; deafen: GamingChord; ptt: GamingChord; games: string[] }
export interface GamingMember { id: string; name: string; speaking: boolean; muted: boolean; sharing: boolean }
export interface GamingVoice { clock: number; scope: string | null; connected: boolean; account: string | null; channel: string | null; muted: boolean; deafened: boolean; sharing: boolean; ptt_mode: boolean; members: GamingMember[] }
export interface GamingSnapshot { active: boolean; overlay: boolean; settings: GamingSettings; voice: GamingVoice }
export function gamingSurface(): boolean {
  return ['sidepeek', 'gaming-overlay'].includes(String(Reflect.get(window, '__MNEMA_GAMING_SURFACE__')))
}
export function startDesktopGaming(): () => void {
  const token: unknown = Reflect.get(window, '__MNEMA_GAMING_BRIDGE__')
  if (typeof token !== 'string' || window.location.protocol !== 'https:') return () => {}
  const auth = useAuthStore(); const chat = useChatStore(); const voice = useVoiceStore()
  const { stopScreenShare } = useWebRTC()
  let alive = true; let pending = false
  let scope = crypto.randomUUID()
  const ready = () => !!auth.user?.id && !!voice.currentChannelId && chat.isConnected && voice.isConnected && !!voice.rtcStats?.connected && Object.values(voice.channelUsers[voice.currentChannelId] ?? {}).some(u => u.id === auth.user?.id)
  const stopScope = watch([() => auth.user, () => chat.isConnected, () => voice.currentChannelId, () => voice.isConnected, ready], () => { scope = crypto.randomUUID(); releasePtt() }, { flush: 'sync' })
  let release: ReturnType<typeof setTimeout> | undefined
  const releasePtt = () => { clearTimeout(release); voice.isPttPressed = false; setGamingPttLease(false) }
  function onControl(event: Event) {
    if (!(event instanceof CustomEvent)) return
    const detail: unknown = event.detail
    if (!detail || typeof detail !== 'object' || Reflect.get(detail, 'token') !== token) return
    const action: unknown = Reflect.get(detail, 'action')
    if (action === 'ptt-release') { releasePtt(); return }
    const issued: unknown = Reflect.get(detail, 'issued'); const expires: unknown = Reflect.get(detail, 'expires'); const now = performance.now()
    if (typeof issued !== 'number' || typeof expires !== 'number' || !Number.isFinite(issued) || !Number.isFinite(expires) || expires <= issued || expires - issued > 151 || now < issued || now >= expires) return
    if (Reflect.get(detail, 'scope') !== scope || !ready()) return
    if (action === 'mute') voice.toggleMute()
    else if (action === 'deafen') voice.toggleDeafen()
    else if (action === 'stream') {
      if (voice.isScreenSharing) stopScreenShare()
      else voice.showScreenShareModal = true // The existing chooser supplies the capture gesture.
    } else if (action === 'ptt-mode') {
      releasePtt(); voice.inputMode = voice.inputMode === 'ptt' ? 'activity' : 'ptt'; voice.saveSettings(); setGamingPttLease(false)
    } else if (action === 'ptt-press' && voice.inputMode === 'ptt') {
      setGamingPttLease(true); voice.isPttPressed = true
      clearTimeout(release); release = setTimeout(releasePtt, 300)
    }
  }
  async function sync() {
    if (!alive || pending) return
    pending = true
    try {
      const account = auth.user?.id ?? null; const channel = voice.currentChannelId
      const users = channel ? Object.values(voice.channelUsers[channel] ?? {}) : []
      const connected = ready()
      if (!connected) releasePtt()
      const state: GamingVoice = {
        clock: performance.now(), scope, connected, account, channel, muted: voice.isMuted, deafened: voice.isDeafened,
        sharing: voice.isScreenSharing, ptt_mode: voice.inputMode === 'ptt',
        members: connected ? users.slice(0, 256).map(u => ({ id: u.id, name: (u.display_name || u.username).slice(0, 80), speaking: voice.isSpeaking(u.id), muted: voice.muteStateOf(u.id).muted, sharing: !!voice.mediaState[u.id]?.screen || (u.id === account && voice.isScreenSharing) })) : []
      }
      await invoke('desktop_gaming_sync', { token, voice: state })
    } catch { releasePtt() } finally { pending = false }
  }
  window.addEventListener('mnema-gaming-command', onControl)
  window.addEventListener('pagehide', releasePtt)
  const timer = setInterval(() => { void sync() }, 500)
  void sync()
  return () => { alive = false; stopScope(); clearInterval(timer); releasePtt(); window.removeEventListener('mnema-gaming-command', onControl); window.removeEventListener('pagehide', releasePtt) }
}
export function chordText(chord: GamingChord): string {
  const key = chord.key === 32 ? 'Space' : chord.key >= 112 && chord.key <= 135 ? `F${chord.key - 111}` : String.fromCharCode(chord.key)
  return [chord.control && 'Ctrl', chord.alt && 'Alt', chord.shift && 'Shift', key].filter(Boolean).join('+')
}
export function chordFromKey(event: KeyboardEvent): GamingChord | null {
  let key = 0
  if (/^Key[A-Z]$/.test(event.code)) key = event.code.charCodeAt(3)
  else if (/^Digit[0-9]$/.test(event.code)) key = event.code.charCodeAt(5)
  else if (/^F([1-9]|1[0-9]|2[0-4])$/.test(event.code)) key = 111 + Number(event.code.slice(1))
  else if (event.code === 'Space') key = 32
  if (!key || event.metaKey || (event.altKey && (key === 32 || key === 115))) return null
  return { key, alt: event.altKey, control: event.ctrlKey, shift: event.shiftKey }
}
