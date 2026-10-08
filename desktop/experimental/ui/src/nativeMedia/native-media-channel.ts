// Ignored native-only synthetic qualification bridge. Never a production MLS grant.
export type Role = 'publisher' | 'viewer'
export type BoundMedia = Readonly<{ handle: string; generation: number; channelId: string; publisherUserId: string; selfUserId: string }>
export type MediaAction =
  | { type: 'voice_join'; payload: { channel_id: string } }
  | { type: 'voice_leave' }
  | { type: 'webrtc_answer'; payload: { type: 'answer'; sdp: string } }
  | { type: 'webrtc_candidate'; payload: RTCIceCandidateInit }
  | { type: 'webrtc_subscribe'; payload: { kind: 'screen'; user_id: string; on: boolean } }
export type MediaMessage =
  | { type: 'offer'; payload: RTCSessionDescriptionInit }
  | { type: 'ice'; payload: RTCIceCandidateInit }
export type NativeMediaEvent = Readonly<{ handle: string; generation: number; sequence: number; type: 'message' | 'closed' | 'error'; message?: MediaMessage }>
export interface NativeMediaBridge {
  // Role is the only renderer choice. Native fixture policy owns all identities,
  // channel/session/capability binding and denies use outside its compile-time lane.
  open(role: Role): Promise<BoundMedia>
  listen(receive: (event: NativeMediaEvent) => void): Promise<() => void>
  send(binding: Pick<BoundMedia, 'handle' | 'generation'>, action: MediaAction): Promise<void>
  close(binding: Pick<BoundMedia, 'handle' | 'generation'>): Promise<void>
}
const canonicalId = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const opaqueHandle = /^[0-9a-f]{64}$/
function bound(value: BoundMedia) {
  return opaqueHandle.test(value.handle) && Number.isSafeInteger(value.generation) && value.generation > 0 &&
    [value.channelId, value.publisherUserId, value.selfUserId].every(id => canonicalId.test(id) && id !== '00000000-0000-0000-0000-000000000000')
}
function messageValid(message: MediaMessage | undefined): message is MediaMessage {
  if (!message) return false
  if (message.type === 'offer') return message.payload?.type === 'offer' && typeof message.payload.sdp === 'string' && message.payload.sdp.length > 0 && message.payload.sdp.length <= 49152
  if (message.type !== 'ice') return false
  const p = message.payload
  return typeof p?.candidate === 'string' && p.candidate.length <= 4096 &&
    (p.sdpMid === undefined || p.sdpMid === null || typeof p.sdpMid === 'string' && p.sdpMid.length <= 64) &&
    (p.sdpMLineIndex === undefined || p.sdpMLineIndex === null || Number.isInteger(p.sdpMLineIndex) && p.sdpMLineIndex >= 0 && p.sdpMLineIndex <= 32) &&
    (p.usernameFragment === undefined || p.usernameFragment === null || typeof p.usernameFragment === 'string' && p.usernameFragment.length <= 256)
}
export class NativeMediaChannel {
  private active = true
  private binding: BoundMedia | undefined
  private unsubscribe: (() => void) | undefined
  private closePromise?: Promise<void>
  private opening?: Promise<BoundMedia | undefined>
  private sequence = 0
  private pending: NativeMediaEvent[] = []
  constructor(private readonly bridge: NativeMediaBridge, private readonly role: Role,
    private readonly receive: (message: MediaMessage) => void, private readonly stopped: (reason: 'closed' | 'error') => void,
    private readonly opened: (binding: BoundMedia) => void = () => {}) {}
  open(): Promise<BoundMedia | undefined> {
    if (!this.active || this.opening) return Promise.reject(new Error('native_media_open_rejected'))
    this.opening = this.setup()
    return this.opening
  }
  private async setup(): Promise<BoundMedia | undefined> {
    try {
      const unsubscribe = await this.bridge.listen(event => this.event(event))
      if (!this.active) { unsubscribe(); return }
      this.unsubscribe = unsubscribe
      const binding = await this.bridge.open(this.role)
      if (!bound(binding)) throw new Error('native_media_binding_rejected')
      if (!this.active) { await this.bridge.close(binding); return }
      this.binding = Object.freeze({ ...binding })
      this.opened(this.binding)
      for (const event of this.pending.splice(0)) this.event(event)
      if (!this.active) return
      await this.send({ type: 'voice_join', payload: { channel_id: binding.channelId } })
      if (!this.active) return
      if (this.role === 'viewer') await this.send({ type: 'webrtc_subscribe', payload: { kind: 'screen', user_id: binding.publisherUserId, on: true } })
      return this.active ? binding : undefined
    } catch {
      if (this.active) this.stopped('error')
      this.active = false
      this.unsubscribe?.(); this.unsubscribe = undefined
      this.pending.length = 0
      const binding = this.binding; this.binding = undefined
      if (binding) await this.bridge.close(binding)
    }
  }
  private event(event: NativeMediaEvent) {
    if (!this.active) return
    if (!this.binding) {
      if (this.pending.length >= 64) { this.stopped('error'); void this.close(); return }
      this.pending.push(event)
      return
    }
    if (event.handle !== this.binding.handle || event.generation !== this.binding.generation) return
    if (!Number.isSafeInteger(event.sequence) || event.sequence <= this.sequence) { this.stopped('error'); void this.close(); return }
    this.sequence = event.sequence
    if (event.type === 'message' && messageValid(event.message)) { this.receive(event.message); return }
    this.stopped(event.type === 'closed' ? 'closed' : 'error')
    void this.close()
  }
  async send(action: MediaAction): Promise<void> {
    const binding = this.binding
    if (!this.active || !binding) throw new Error('native_media_send_rejected')
    // Renderer checks are defensive only; the native actor must independently
    // validate the same fixed policy and lease immediately before every write.
    if (action.type === 'voice_join' && action.payload.channel_id !== binding.channelId ||
      action.type === 'webrtc_subscribe' && action.payload.user_id !== binding.publisherUserId ||
      action.type === 'webrtc_answer' && (action.payload.type !== 'answer' || action.payload.sdp.length === 0 || action.payload.sdp.length > 49152)) throw new Error('native_media_action_rejected')
    await this.bridge.send(binding, action)
    if (!this.active) throw new Error('native_media_stopped_during_send')
  }
  close(): Promise<void> {
    if (this.closePromise) return this.closePromise
    this.active = false
    this.unsubscribe?.(); this.unsubscribe = undefined
    this.pending.length = 0
    this.closePromise = (async () => {
      await this.opening
      const binding = this.binding; this.binding = undefined
      if (binding) await this.bridge.close(binding)
    })()
    return this.closePromise
  }
}
