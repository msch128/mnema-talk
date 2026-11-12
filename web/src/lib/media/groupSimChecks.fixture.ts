// The invariants the simulator holds the media group protocol to.
//
// 1. Once the relay stops interfering, every member in the room reaches the
//    same epoch and the same media key within a bounded time (GroupChecks.agreed,
//    then sameKeys with the real frame seal and open).
// 2. No identity holds two leaves in any epoch a member takes keys from.
// 3. A tab that left or was reloaded away opens nothing a member seals later:
//    none of the keys it ever held is a member's key at the end.
// 4. No member takes a commit, or a welcome, that breaks the rules under
//    "What a member can do" in SECURITY.md.
//
// The fourth is judged from outside the group: by the wires the relay
// delivered to that member, the roster it was told and its keys before and
// after each wire. Where the outside view cannot be sure (a member's exact
// sign-of-life times), it gives the benefit of the doubt, so every report is
// a real breach and some breaches may go unseen.
import { decodeMlsMessage, encodeMlsMessage, type ClientState, type MLSMessage, type PublicMessage } from 'ts-mls'
import { openFrame, ReplayWindow, sealFrame, type EpochKeys } from './frame'
import { credentialIdentity, sameBytes, treeLeafSigner } from './groupTree'
import { isCapability } from './capability'
import { isHeartbeat, openHeartbeat } from './heartbeat'
import { mediaGroupId } from './bounds'
import type { MediaVerification } from './verify'
import { epochKey, memberNumber, SIM_CHANNEL, SIM_TIMING, type GroupSim, type SimHooks, type SimMember } from './groupSim.fixture'

const SLACK = Math.max(10, SIM_TIMING.maintenanceMs)
const PATIENCE = SIM_TIMING.absentMs + 2 * SLACK
const SLEEP_LIMIT = 2 * SLACK + 60_000
const RENEWAL_GAP = Math.max(0, SIM_TIMING.rotateMs - SIM_TIMING.rotateJitterMs) / 2
const GROUP_ID = mediaGroupId(SIM_CHANNEL) as Uint8Array

/** What one member was shown, as far as the relay can tell. */
interface Seen {
  /** The latest sign of life of each member of its group that is certain: never later than the group's own view. */
  readonly alive: Map<string, number>
  readonly beats: Map<string, number>
  /** Members of its group that sent a key package since its last epoch: they may be coming back. */
  readonly restarting: Set<string>
  /** Key packages the relay delivered to it, by sender. */
  readonly relayed: Map<string, Uint8Array[]>
  readonly renewals: Map<string, number>
  /** Members it got any handshake from. A lower id it never heard from may be left out when it looks for the committer. */
  readonly heard: Set<string>
}

interface Effects { adds: { id: string | null, wire: Uint8Array }[], removed: string[], empty: boolean }

function decode(wire: Uint8Array): MLSMessage | null {
  try {
    return decodeMlsMessage(wire, 0)?.[0] ?? null
  } catch {
    return null
  }
}

/** The checks pick the lowest id themselves: they must not share a tie-break with what they check. */
function lowest(ids: readonly string[]): string | null {
  return ids.length ? ids.reduce((best, id) => (id < best ? id : best)) : null
}

function name(id: string | null): string {
  return id ? `m${memberNumber(id)}` : 'nobody'
}

/** What a commit's proposals do, or null when one of them is neither an add nor a remove written into the commit. */
function effectsOf(message: PublicMessage, before: MediaVerification, after: MediaVerification): Effects | null {
  const content = message.content
  if (content.contentType !== 'commit') return null
  const adds: Effects['adds'] = []
  for (const item of content.commit.proposals) {
    if (item.proposalOrRefType !== 'proposal') return null
    const proposal = item.proposal
    if (proposal.proposalType === 'remove') continue
    if (proposal.proposalType !== 'add') return null
    const keyPackage = proposal.add.keyPackage
    adds.push({ id: credentialIdentity(keyPackage.leafNode.credential), wire: encodeMlsMessage({ version: 'mls10', wireformat: 'mls_key_package', keyPackage }) })
  }
  const removed = before.members.filter(id => !after.members.includes(id))
  return { adds, removed, empty: content.commit.proposals.length === 0 }
}

/**
 * The committer took nothing: a renewal for an epoch it had already committed
 * on top of came late, and it answered with a commit of its own, which moves
 * its group one epoch on with the same members.
 */
function answeredLate(message: PublicMessage, before: MediaVerification, after: MediaVerification): boolean {
  return message.content.epoch < BigInt(before.epoch - 1) && after.epoch === before.epoch + 1 && after.members.join(',') === before.members.join(',')
}

/** Whether `candidate` is the AES key `holder` uses: sealed with one, opened with the other, by the real frame code. */
async function sameAesKey(candidate: EpochKeys, holder: EpochKeys, holderId: string, epoch: number): Promise<boolean> {
  const keys: EpochKeys = { key: candidate.key, signer: holder.signer, verifiers: holder.verifiers }
  const packet = await sealFrame({ keys, epoch, stream: 1, counter: 0, channelId: SIM_CHANNEL, senderId: holderId, plaintext: new Uint8Array([1, 2, 3]), source: 0 })
  if (!packet) return false
  const opened = await openFrame({ keys: new Map([[epoch, holder]]), channelId: SIM_CHANNEL, senderId: holderId, packet, replay: new ReplayWindow(), source: 0 })
  return opened !== null
}

export class GroupChecks implements SimHooks {
  private sim: GroupSim | null = null
  private readonly seen = new WeakMap<SimMember, Seen>()

  bind(sim: GroupSim) {
    this.sim = sim
  }

  private get world(): GroupSim {
    if (!this.sim) throw new Error('checks are not bound to a simulation')
    return this.sim
  }

  private of(member: SimMember): Seen {
    let seen = this.seen.get(member)
    if (!seen) {
      seen = { alive: new Map(), beats: new Map(), restarting: new Set(), relayed: new Map(), renewals: new Map(), heard: new Set() }
      this.seen.set(member, seen)
    }
    return seen
  }

  private fail(member: SimMember, text: string) {
    this.world.violation(`${member.label} ${text}`)
  }

  /** Invariant 2, and the bookkeeping of who is new to this member's group. */
  epoch(member: SimMember, verification: MediaVerification | null) {
    const seen = this.of(member)
    seen.restarting.clear()
    if (!verification) {
      seen.alive.clear()
      seen.beats.clear()
      seen.renewals.clear()
      return
    }
    const members = verification.members
    if (members.includes('') || new Set(members).size !== members.length) this.fail(member, `[2] took keys from an epoch where an identity holds two leaves: ${members.map(name).join(',')}`)
    if (!members.includes(member.id)) this.fail(member, '[2] took keys from an epoch it has no leaf in')
    for (const id of [...seen.alive.keys()]) {
      if (members.includes(id)) continue
      seen.alive.delete(id)
      seen.beats.delete(id)
      seen.renewals.delete(id)
    }
    for (const id of members) if (!seen.alive.has(id)) seen.alive.set(id, this.world.now)
  }

  async received(member: SimMember, from: string, wire: Uint8Array, before: MediaVerification | null, after: MediaVerification | null) {
    const seen = this.of(member)
    if (isHeartbeat(wire)) return this.heartbeat(member, seen, from, wire, before)
    if (isCapability(wire)) return
    const message = decode(wire)
    if (!message) return
    seen.heard.add(from)
    if (message.wireformat === 'mls_key_package') return this.keyPackage(member, seen, from, wire, message.keyPackage.leafNode.signaturePublicKey, before)
    // Only a wire that gave the member other keys was taken.
    if (!after || epochKey(before) === epochKey(after)) return
    if (message.wireformat === 'mls_welcome') {
      // A group taken by welcome is a new group: the call forgets its renewal times with the old one (a rival welcome discards it without a "keys none" in between).
      seen.renewals.clear()
      this.welcome(member, seen, from, before, after)
    }
    else if (message.wireformat === 'mls_public_message') this.commit(member, seen, from, message.publicMessage, before, after)
  }

  /**
   * A member of the group that sends a package with another signature key than
   * its leaf has is coming back (a reload). The package it is in the group with,
   * sent again, says nothing: a group of one repeats it on every tick.
   */
  private keyPackage(member: SimMember, seen: Seen, from: string, wire: Uint8Array, signer: Uint8Array, before: MediaVerification | null) {
    seen.relayed.set(from, [...(seen.relayed.get(from) ?? []), new Uint8Array(wire)])
    if (!before?.members.includes(from)) return
    const state = (member.group as unknown as { state: ClientState | null } | null)?.state
    const leaf = state ? treeLeafSigner(state, from) : null
    if (!leaf || !sameBytes(leaf, signer)) seen.restarting.add(from)
  }

  /** A heartbeat is a sign of life when the sender's leaf of this epoch signed it and it is newer than the last. */
  private async heartbeat(member: SimMember, seen: Seen, from: string, wire: Uint8Array, before: MediaVerification | null) {
    const held = member.keys.at(-1)
    if (!before || !held || epochKey(member.current) !== epochKey(before)) return
    const verifier = held.key.verifiers.get(from)
    const beat = verifier ? await openHeartbeat(verifier, GROUP_ID, from, wire) : null
    if (!beat || beat.epoch !== before.epoch || beat.at <= (seen.beats.get(from) ?? -Infinity)) return
    seen.beats.set(from, beat.at)
    seen.alive.set(from, this.world.now)
  }

  /**
   * A welcome is taken only from the committer of the group it leads into: the
   * lowest of its members in the room, leaving out lower ids this member never
   * heard a word from (the group writes those off after a while).
   */
  private welcome(member: SimMember, seen: Seen, from: string, before: MediaVerification | null, after: MediaVerification) {
    const room = new Set(member.roster)
    const known = (id: string) => id > member.id || seen.heard.has(id) || before?.members.includes(id) === true
    const committer = lowest(after.members.filter(id => id !== member.id && room.has(id) && (id === from || known(id))))
    if (committer !== from) this.fail(member, `[4] took a welcome from ${name(from)}, but ${name(committer)} commits in that group`)
  }

  private commit(member: SimMember, seen: Seen, from: string, message: PublicMessage, before: MediaVerification | null, after: MediaVerification) {
    if (!before) return this.fail(member, `[4] took a commit from ${name(from)} without holding a group`)
    if (answeredLate(message, before, after)) return
    const effects = effectsOf(message, before, after)
    if (!effects) return this.fail(member, `[4] took a commit from ${name(from)} that does more than add or remove`)
    if (!before.members.includes(from)) this.fail(member, `[4] took a commit from ${name(from)}, who is not in its group`)
    this.checkStep(member, from, message, before, after, effects)
    this.checkSender(member, seen, from, before, effects)
    this.checkAdds(member, seen, from, effects)
    this.checkRemoves(member, seen, effects)
    const now = this.world.now
    seen.alive.set(from, now)
    for (const add of effects.adds) {
      // A member that came back starts with a clean slate, like the group gives it.
      if (add.id && before.members.includes(add.id)) {
        seen.alive.set(add.id, now)
        seen.beats.delete(add.id)
      }
    }
  }

  /**
   * A commit moves the group one epoch on, and whoever is new in it is someone
   * the commit adds. The one exception is the commit that wins against a
   * renewal this member took for the same epoch: it leads from the epoch
   * before to the one the member is in, and a renewal changed no member.
   */
  private checkStep(member: SimMember, from: string, message: PublicMessage, before: MediaVerification, after: MediaVerification, effects: Effects) {
    const rollback = message.content.epoch === BigInt(before.epoch - 1)
    const fits = rollback ? after.epoch === before.epoch : message.content.epoch === BigInt(before.epoch) && after.epoch === before.epoch + 1
    if (!fits) this.fail(member, `[4] took a commit from ${name(from)} for epoch ${message.content.epoch} that led from epoch ${before.epoch} to ${after.epoch}`)
    const added = new Set(effects.adds.map(add => add.id))
    for (const id of after.members) {
      if (!before.members.includes(id) && !added.has(id)) this.fail(member, `[4] took a commit from ${name(from)} after which ${name(id)} is in the group, whom it does not add`)
    }
    for (const id of added) {
      if (id && !after.members.includes(id)) this.fail(member, `[4] took a commit from ${name(from)} that adds ${name(id)}, who is not in the group after it`)
    }
  }

  /** Certain to count as a live member of the group for `member`, by signs of life the relay delivered. */
  private surelyActive(member: SimMember, seen: Seen, id: string): boolean {
    if (id === member.id) return true
    if (seen.restarting.has(id)) return false
    const alive = seen.alive.get(id)
    return alive !== undefined && this.world.now - alive <= PATIENCE
  }

  /** The commit comes from the member who commits now, or holds nothing but its sender's own renewal, and that not too often. */
  private checkSender(member: SimMember, seen: Seen, from: string, before: MediaVerification, effects: Effects) {
    const room = new Set([...member.roster, member.id])
    const lower = before.members.find(id => id < from && room.has(id) && this.surelyActive(member, seen, id))
    if (lower === undefined) {
      if (!effects.empty && !room.has(from)) this.fail(member, `[4] took a commit from ${name(from)}, who is not in the room`)
      return
    }
    if (!effects.empty) return this.fail(member, `[4] took a commit from ${name(from)} although ${name(lower)} commits now`)
    const now = this.world.now
    const last = seen.renewals.get(from)
    if (last !== undefined && now - last < RENEWAL_GAP) this.fail(member, `[4] took a second renewal from ${name(from)} after ${now - last} ms`)
    seen.renewals.set(from, now)
  }

  /** An add is of an identity in the room, with the very key package the relay delivered from it, and not twice. */
  private checkAdds(member: SimMember, seen: Seen, from: string, effects: Effects) {
    const room = new Set([...member.roster, ...member.previousRoster])
    const added = new Set<string>()
    for (const { id, wire } of effects.adds) {
      if (!id || id === from || added.has(id)) {
        this.fail(member, `[4] took a commit from ${name(from)} that adds ${name(id)} (itself, twice or unreadable)`)
        continue
      }
      added.add(id)
      if (!room.has(id)) this.fail(member, `[4] took the add of ${name(id)}, who is not in the room`)
      if (!(seen.relayed.get(id) ?? []).some(sent => sameBytes(sent, wire))) this.fail(member, `[4] took the add of ${name(id)} with a key package the relay never delivered`)
    }
  }

  /** A remove is of a member the roster no longer lists or one that was silent; a member that slept takes it on trust. */
  private checkRemoves(member: SimMember, seen: Seen, effects: Effects) {
    const now = this.world.now
    if (now >= member.wokeAt && now - member.wokeAt <= 2 * SLEEP_LIMIT) return
    for (const id of effects.removed) {
      if (!member.roster.includes(id)) continue
      const alive = seen.alive.get(id)
      if (alive !== undefined && now - alive <= SIM_TIMING.absentMs - SLACK) this.fail(member, `[4] took the removal of ${name(id)}, who is in the room and was heard ${now - alive} ms ago`)
    }
  }

  /** Invariant 1 by safety code: everyone in the room holds the same epoch, with exactly the room in it. */
  agreed(): boolean {
    const live = this.world.live()
    const ids = live.map(member => member.id).sort().join(',')
    const first = live[0]
    if (!first) return false
    return live.every(member => member.current !== null && member.staleAt === null
      && epochKey(member.current) === epochKey(first.current) && member.current.members.join(',') === ids)
  }

  describe(): string {
    return this.world.live().map(member => `${member.label}:${epochKey(member.current) ?? 'none'}{${(member.current?.members ?? []).map(memberNumber).join(',')}}${member.staleAt === null ? '' : ' (no group)'}`).join(' | ')
  }

  /** Invariant 1 for real: what each member seals, every other member opens with the key of its current epoch. */
  async sameKeys() {
    const live = this.world.live()
    for (const sender of live) {
      const sealing = sender.keys.at(-1)
      if (!sealing) continue
      const packet = await sealFrame({ keys: sealing.key, epoch: sealing.epoch, stream: 1, counter: 0, channelId: SIM_CHANNEL, senderId: sender.id, plaintext: new Uint8Array([1, 2, 3]), source: 0 })
      for (const receiver of live) {
        const held = receiver.keys.at(-1)
        if (receiver === sender || !held || !packet) continue
        const opened = await openFrame({ keys: new Map([[held.epoch, held.key]]), channelId: SIM_CHANNEL, senderId: sender.id, packet, replay: new ReplayWindow(), source: 0 })
        if (!opened) this.fail(receiver, `[1] cannot open what ${sender.label} seals`)
      }
    }
  }

  /** Invariant 3: no key a departed tab ever held is the key a member seals with now. */
  async departedHoldNothing() {
    const live = this.world.live()
    for (const departed of this.world.all) {
      if (!departed.gone) continue
      for (const member of live) {
        const current = member.keys.at(-1)
        if (!current) continue
        for (const held of departed.keys) {
          if (await sameAesKey(held.key, current.key, member.id, current.epoch)) this.fail(departed, `[3] left, and still holds the key ${member.label} seals with (epoch ${current.epoch})`)
        }
      }
    }
  }
}
