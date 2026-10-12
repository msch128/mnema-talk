import {
  acceptAll,
  createCommit,
  createGroup,
  decodeMlsMessage,
  defaultCapabilities,
  defaultKeyPackageEqualityConfig,
  defaultKeyRetentionConfig,
  defaultLifetime,
  defaultLifetimeConfig,
  defaultPaddingConfig,
  emptyPskIndex,
  encodeMlsMessage,
  generateKeyPackage,
  getCiphersuiteFromName,
  getCiphersuiteImpl,
  joinGroup,
  mlsExporter,
  processPublicMessage,
  zeroOutUint8Array,
  type CiphersuiteImpl,
  type ClientConfig,
  type ClientState,
  type Credential,
  type KeyPackage,
  type PrivateKeyPackage,
  type Proposal,
} from 'ts-mls'
import { FRAME_EPOCH_BYTES, HANDSHAKE_MAX_BYTES, MEDIA_EXPORTER_LABEL, mediaGroupId, mediaUserId } from './bounds'
import { importEpochKey } from './frame'

export interface MediaStep {
  wires: Uint8Array[]
  epoch: number | null
  key: CryptoKey | null
}

interface OwnPackage {
  publicPackage: KeyPackage
  privatePackage: PrivateKeyPackage
}

const emptyStep = (epoch: number | null, key: CryptoKey | null): MediaStep => ({ wires: [], epoch, key })

let suitePromise: Promise<CiphersuiteImpl> | null = null

function suite(): Promise<CiphersuiteImpl> {
  suitePromise ??= getCiphersuiteImpl(getCiphersuiteFromName('MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519'))
  return suitePromise
}

function credentialIdentity(credential: Credential): string | null {
  if (credential.credentialType !== 'basic') return null
  const text = new TextDecoder().decode(credential.identity)
  return mediaUserId(text) === text ? text : null
}

function clientConfig(): ClientConfig {
  return {
    keyRetentionConfig: defaultKeyRetentionConfig,
    lifetimeConfig: defaultLifetimeConfig,
    keyPackageEqualityConfig: defaultKeyPackageEqualityConfig,
    paddingConfig: defaultPaddingConfig,
    authService: {
      validateCredential(credential: Credential): Promise<boolean> {
        return Promise.resolve(credentialIdentity(credential) !== null)
      },
    },
  }
}

function sameBytes(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) return false
  for (let i = 0; i < left.byteLength; i++) if (left[i] !== right[i]) return false
  return true
}

function wipeBytes(bytes: Uint8Array | undefined) {
  if (bytes) zeroOutUint8Array(bytes)
}

function wipeConsumed(consumed: Uint8Array[]) {
  for (const secret of consumed) wipeBytes(secret)
}

function treeMembers(state: ClientState): Map<string, number> {
  const members = new Map<string, number>()
  state.ratchetTree.forEach((node, index) => {
    if (!node || node.nodeType !== 'leaf' || index % 2 !== 0) return
    const id = credentialIdentity(node.leaf.credential)
    if (id) members.set(id, index / 2)
  })
  return members
}

function lowest(ids: Iterable<string>): string | null {
  let best: string | null = null
  for (const id of ids) if (best === null || id < best) best = id
  return best
}

function encodeWire(message: Parameters<typeof encodeMlsMessage>[0]): Uint8Array | null {
  try {
    const bytes = encodeMlsMessage(message)
    return bytes.byteLength > 0 && bytes.byteLength <= HANDSHAKE_MAX_BYTES ? bytes : null
  } catch {
    return null
  }
}

/**
 * One browser's membership in one voice channel.
 * The lowest user id in the room creates the group and publishes commits.
 * Anyone higher only offers a key package. A group that does not yet contain
 * a lower id is dropped, and that lower id creates the replacement. A member
 * who leaves is removed by whoever remains.
 */
export class MediaGroup {
  private state: ClientState | null = null
  private readonly pending = new Map<string, KeyPackage>()
  private readonly keys = new Map<number, CryptoKey>()
  private current: { epoch: number, key: CryptoKey } | null = null
  private room: readonly string[] = []
  private offeredFor: string | null = null
  /** Key-package wire we already added, so the same offer can be welcomed again. */
  private readonly accepted = new Map<string, Uint8Array>()
  private closed = false
  private tail: Promise<void> = Promise.resolve()

  private constructor(
    private readonly userId: string,
    private readonly groupId: Uint8Array,
    private readonly own: OwnPackage,
    private readonly cs: CiphersuiteImpl,
  ) {}

  static async open(userId: string, channelId: string): Promise<MediaGroup | null> {
    const id = mediaUserId(userId)
    const groupId = mediaGroupId(channelId)
    if (!id || !groupId) return null
    const cs = await suite()
    const credential: Credential = { credentialType: 'basic', identity: new TextEncoder().encode(id) }
    const own = await generateKeyPackage(credential, defaultCapabilities(), defaultLifetime, [], cs)
    return new MediaGroup(id, groupId, own, cs)
  }

  sync(roster: readonly string[], resend = false): Promise<MediaStep> {
    return this.enqueue(() => this.syncNow(roster, resend))
  }

  receive(fromUserId: string, wire: Uint8Array): Promise<MediaStep> {
    return this.enqueue(() => this.receiveNow(fromUserId, wire))
  }

  close(): Promise<void> {
    return this.enqueue(async () => {
      this.closed = true
      this.discard()
      wipeBytes(this.own.privatePackage.initPrivateKey)
      wipeBytes(this.own.privatePackage.hpkePrivateKey)
      wipeBytes(this.own.privatePackage.signaturePrivateKey)
    })
  }

  private enqueue<T>(job: () => Promise<T>): Promise<T> {
    const run = this.tail.then(job)
    this.tail = run.then(() => undefined, () => undefined)
    return run
  }

  private step(): MediaStep {
    return emptyStep(this.current?.epoch ?? null, this.current?.key ?? null)
  }

  private others(roster: readonly string[]): string[] {
    const ids = new Set<string>()
    for (const value of roster) {
      const id = mediaUserId(value)
      if (id && id !== this.userId) ids.add(id)
    }
    return [...ids]
  }

  /** Lowest id that is both in the tree and still in the room. */
  private canCommit(others: readonly string[]): boolean {
    if (!this.state) return false
    const room = new Set(others)
    room.add(this.userId)
    const active: string[] = []
    for (const id of treeMembers(this.state).keys()) if (room.has(id)) active.push(id)
    return lowest(active) === this.userId
  }

  private async syncNow(roster: readonly string[], resend: boolean): Promise<MediaStep> {
    if (this.closed) return this.step()
    const wires: Uint8Array[] = []
    const others = this.others(roster)
    this.room = others
    const state = this.state
    if (state && others.some(id => id < this.userId && !treeMembers(state).has(id))) {
      this.discard()
    }
    if (!this.state && !others.some(id => id < this.userId)) {
      const created = await this.createOwn()
      if (created) wires.push(...created)
    } else if (!this.state) {
      if (resend) this.offeredFor = null
      const wire = this.offer(others)
      if (wire) wires.push(wire)
    }
    if (this.canCommit(others)) {
      wires.push(...await this.commitRemoves(others))
      wires.push(...await this.commitAdds(others))
    }
    return { wires, epoch: this.current?.epoch ?? null, key: this.current?.key ?? null }
  }

  private offer(others: string[]): Uint8Array | null {
    const key = others.slice().sort().join(',')
    if (this.offeredFor === key) return null
    const wire = encodeWire({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: this.own.publicPackage })
    if (!wire) return null
    this.offeredFor = key
    return wire
  }

  private async createOwn(): Promise<Uint8Array[] | null> {
    const state = await createGroup(this.groupId, this.own.publicPackage, this.own.privatePackage, [], this.cs, clientConfig())
    if (!await this.adopt(state)) return null
    this.offeredFor = null
    return []
  }

  private discard() {
    const state = this.state
    this.state = null
    this.current = null
    this.keys.clear()
    this.pending.clear()
    this.accepted.clear()
    this.offeredFor = null
    if (state) this.wipeRejected(state)
  }

  private async receiveNow(fromUserId: string, wire: Uint8Array): Promise<MediaStep> {
    if (this.closed || wire.byteLength === 0 || wire.byteLength > HANDSHAKE_MAX_BYTES) return this.step()
    const from = mediaUserId(fromUserId)
    if (!from || from === this.userId) return this.step()
    const decoded = decodeMlsMessage(wire, 0)
    if (!decoded) return this.step()
    const [message] = decoded
    const members = this.state ? treeMembers(this.state) : null
    if (members && from < this.userId && !members.has(from)) this.discard()
    if (message.wireformat === 'mls_key_package') {
      const id = credentialIdentity(message.keyPackage.leafNode.credential)
      if (id !== from) return this.step()
      const members = this.state ? treeMembers(this.state) : null
      if (members?.has(from)) {
        const previous = this.accepted.get(from)
        if (!previous || !sameBytes(previous, wire) || !this.canCommit(this.room)) return this.step()
        this.pending.set(from, message.keyPackage)
        const removed = await this.commitRemoves(this.room.filter(member => member !== from))
        const added = await this.commitAdds([from])
        return { wires: [...removed, ...added], epoch: this.current?.epoch ?? null, key: this.current?.key ?? null }
      }
      this.accepted.set(from, new Uint8Array(wire))
      this.pending.set(from, message.keyPackage)
      if (this.canCommit(this.room)) {
        const wires = await this.commitAdds([from])
        return { wires, epoch: this.current?.epoch ?? null, key: this.current?.key ?? null }
      }
      return this.step()
    }
    if (message.wireformat === 'mls_welcome') {
      if (this.state) return this.step()
      const committer = lowest(this.room)
      if (from >= this.userId || committer === null || from !== committer) return this.step()
      try {
        const joined = await joinGroup(message.welcome, this.own.publicPackage, this.own.privatePackage, emptyPskIndex, this.cs)
        if (await this.adopt(joined)) this.offeredFor = null
      } catch {
        return this.step()
      }
      return this.step()
    }
    if (message.wireformat === 'mls_public_message' && this.state) {
      const previous = this.state
      try {
        const processed = await processPublicMessage(previous, message.publicMessage, emptyPskIndex, this.cs, acceptAll)
        wipeConsumed(processed.consumed)
        // A commit that removes us is the signal to take the welcome that follows.
        if (!treeMembers(processed.newState).has(this.userId)) {
          this.wipeRejected(processed.newState)
          this.discard()
          return this.step()
        }
        await this.adopt(processed.newState)
      } catch {
        return this.step()
      }
    }
    return this.step()
  }

  private async commitRemoves(others: readonly string[]): Promise<Uint8Array[]> {
    if (!this.state) return []
    const present = new Set(others)
    present.add(this.userId)
    const proposals = []
    for (const [id, leaf] of treeMembers(this.state)) {
      if (!present.has(id)) proposals.push({ proposalType: 'remove' as const, remove: { removed: leaf } })
    }
    if (!proposals.length) return []
    return this.commit(proposals)
  }

  private async commitAdds(others: readonly string[]): Promise<Uint8Array[]> {
    if (!this.state) return []
    const wanted = new Set(others)
    const members = treeMembers(this.state)
    const proposals = []
    for (const [id, keyPackage] of this.pending) {
      if (!wanted.has(id) || members.has(id)) continue
      proposals.push({ proposalType: 'add' as const, add: { keyPackage } })
    }
    if (!proposals.length) return []
    const wires = await this.commit(proposals)
    if (wires.length) {
      for (const proposal of proposals) {
        const id = credentialIdentity(proposal.add.keyPackage.leafNode.credential)
        if (id) this.pending.delete(id)
      }
    }
    return wires
  }

  private async commit(extraProposals: Proposal[]): Promise<Uint8Array[]> {
    if (!this.state) return []
    const previous = this.state
    let result: Awaited<ReturnType<typeof createCommit>>
    try {
      result = await createCommit(
        { state: previous, cipherSuite: this.cs },
        { extraProposals, wireAsPublicMessage: true, ratchetTreeExtension: true },
      )
    } catch {
      return []
    }
    wipeConsumed(result.consumed)
    const adding = extraProposals.some(proposal => proposal.proposalType === 'add')
    const welcome = result.welcome
      ? encodeWire({ version: 'mls10', wireformat: 'mls_welcome', welcome: result.welcome })
      : null
    const commit = result.commit.wireformat === 'mls_public_message' ? encodeWire(result.commit) : null
    if (!commit || (adding && !welcome)) {
      this.wipeRejected(result.newState)
      return []
    }
    if (!await this.adopt(result.newState)) return []
    return welcome ? [welcome, commit] : [commit]
  }

  /** Drops secrets of a state we refuse, without erasing keys the current group still uses. */
  private wipeRejected(state: ClientState) {
    const kept = new Set<Uint8Array>()
    const keep = (bytes: Uint8Array | undefined) => { if (bytes) kept.add(bytes) }
    keep(this.own.privatePackage.initPrivateKey)
    keep(this.own.privatePackage.hpkePrivateKey)
    keep(this.own.privatePackage.signaturePrivateKey)
    if (this.state) {
      const schedule = this.state.keySchedule
      for (const secret of [
        schedule.exporterSecret, schedule.senderDataSecret, schedule.externalSecret, schedule.confirmationKey,
        schedule.membershipKey, schedule.resumptionPsk, schedule.epochAuthenticator, schedule.initSecret,
        this.state.signaturePrivateKey, this.state.confirmationTag,
      ]) keep(secret)
      for (const secret of Object.values(this.state.privatePath.privateKeys)) keep(secret)
    }
    const wipe = (bytes: Uint8Array | undefined) => { if (bytes && !kept.has(bytes)) zeroOutUint8Array(bytes) }
    const schedule = state.keySchedule
    for (const secret of [
      schedule.exporterSecret, schedule.senderDataSecret, schedule.externalSecret, schedule.confirmationKey,
      schedule.membershipKey, schedule.resumptionPsk, schedule.epochAuthenticator, schedule.initSecret,
      state.signaturePrivateKey, state.confirmationTag,
    ]) wipe(secret)
    for (const secret of Object.values(state.privatePath.privateKeys)) wipe(secret)
  }

  private async adopt(state: ClientState): Promise<boolean> {
    if (this.closed || !sameBytes(state.groupContext.groupId, this.groupId)) {
      this.wipeRejected(state)
      return false
    }
    const epoch = state.groupContext.epoch
    if (epoch < 0n || epoch > 0xffffffffn) {
      this.wipeRejected(state)
      return false
    }
    const raw = await mlsExporter(state.keySchedule.exporterSecret, MEDIA_EXPORTER_LABEL, this.groupId, FRAME_EPOCH_BYTES, this.cs)
    try {
      const key = await importEpochKey(raw)
      if (!key || !treeMembers(state).has(this.userId)) {
        this.wipeRejected(state)
        return false
      }
      this.state = state
      const epochNumber = Number(epoch)
      this.current = { epoch: epochNumber, key }
      this.keys.set(epochNumber, key)
      const ordered = [...this.keys.keys()].sort((a, b) => a - b)
      for (const drop of ordered.slice(0, -2)) this.keys.delete(drop)
      return true
    } finally {
      zeroOutUint8Array(raw)
    }
  }
}
