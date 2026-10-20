import {
  createGroup,
  defaultCapabilities,
  defaultLifetime,
  generateKeyPackage,
} from 'ts-mls'
import { clock } from './clock'
import { OFFER_WAIT_MS, FOUND_WAIT_MS, TAKEOVER_MS, type MediaStep } from './groupParams'
import { clientConfig, wipeBytes, treeMembers, lowest, encodeWire } from './groupTree'
import { GroupEpoch } from './groupEpoch'

export abstract class GroupSync extends GroupEpoch {
  sync(roster: readonly string[], resend = false): Promise<MediaStep> {
    this.hint(roster)
    return this.enqueue(() => this.syncNow(roster, resend))
  }

  close(): Promise<void> {
    return this.enqueue(async () => {
      this.closed = true
      for (const timer of [this.deferTimer, this.offerTimer, this.takeoverTimer, this.tickTimer]) if (timer !== null) clearTimeout(timer)
      this.deferTimer = this.offerTimer = this.takeoverTimer = this.tickTimer = null
      this.discard()
      wipeBytes(this.own.privatePackage.initPrivateKey)
      wipeBytes(this.own.privatePackage.hpkePrivateKey)
      wipeBytes(this.own.privatePackage.signaturePrivateKey)
    })
  }

  private pruneFirstSeen(others: string[]) {
    for (const id of [...this.firstSeen.keys()]) {
      if (!others.includes(id)) this.firstSeen.delete(id)
    }
  }

  private isKnownOrHigher(id: string, tree: Set<string> | null): boolean {
    if (id > this.userId) return true
    return (tree?.has(id) || this.heard.has(id)) ?? false
  }

  private filterReachableOthers(others: string[], tree: Set<string> | null, now: number): { kept: string[]; wait: number } {
    let wait = Infinity
    const kept = others.filter((id) => {
      if (this.isKnownOrHigher(id, tree)) return true
      const since = this.firstSeen.get(id) ?? now
      this.firstSeen.set(id, since)
      const left = TAKEOVER_MS - (now - since)
      if (left > 0) wait = Math.min(wait, left)
      return left > 0
    })
    return { kept, wait }
  }

  private scheduleTakeover(wait: number) {
    if (this.takeoverTimer !== null) clearTimeout(this.takeoverTimer)
    this.takeoverTimer = null
    if (wait === Infinity) return
    this.takeoverTimer = setTimeout(() => {
      this.takeoverTimer = null
      void this.enqueue(async () => (this.closed ? null : this.syncNow(this.lastRoster, false))).then(step => step && this.stepListener(step))
    }, wait + 50)
  }

  /**
   * Leaves out lower ids the room lists but that have never been heard from
   * after TAKEOVER_MS, so one client that cannot speak MLS does not wedge the
   * room. They count again as soon as they say anything.
   */
  protected reachable(others: string[]): string[] {
    const now = clock.now()
    const tree = this.state ? treeMembers(this.state) : null
    this.pruneFirstSeen(others)
    const { kept, wait } = this.filterReachableOthers(others, tree, now)
    this.scheduleTakeover(wait)
    return kept
  }

  protected defer(action: () => Promise<MediaStep | null>) {
    if (this.deferTimer !== null) return
    this.deferTimer = setTimeout(() => {
      this.deferTimer = null
      void this.enqueue(async () => (this.closed ? null : action())).then(step => step && this.stepListener(step))
    }, this.resets.waitMs('self') + 50)
  }

  /** Offers a fresh package again, if still without a group and the reset limit allows. */
  protected async reofferNow(): Promise<MediaStep | null> {
    if (this.closed || this.state) return null
    // Nobody let me in although I am the lowest: nobody here holds a group.
    if (!this.room.some(id => id < this.userId)) return this.foundNow()
    if (!this.resets.allow('self')) {
      this.defer(() => this.reofferNow())
      return null
    }
    await this.rekey()
    return { wires: this.offer(this.room), epoch: null, key: null, verification: null }
  }

  protected watchOffer(wait = this.room.some(id => id < this.userId) ? OFFER_WAIT_MS : FOUND_WAIT_MS) {
    if (this.offerTimer !== null) clearTimeout(this.offerTimer)
    this.offerTimer = setTimeout(() => {
      this.offerTimer = null
      void this.enqueue(() => this.reofferNow()).then(step => step && this.stepListener(step))
    }, wait)
  }

  /** Takes the roster: who else is reachable now, remembering when that changed. */
  private refreshRoom(roster: readonly string[]): string[] {
    this.lastRoster = roster
    const others = this.reachable(this.others(roster))
    if (others.length !== this.room.length || others.some(id => !this.room.includes(id))) this.lastRosterAt = clock.now()
    this.room = others
    for (const id of [...this.relayed.keys()]) if (!others.includes(id)) this.relayed.delete(id)
    return others
  }

  /** Founds a group when everyone offered and nobody is lower, or offers a package again. */
  private async establishGroup(others: string[], resend: boolean, wires: Uint8Array[]) {
    if (!this.state && (others.length === 0 || this.allOffered(others))) {
      const created = await this.createOwn()
      if (created) wires.push(...created)
    } else if (!this.state) {
      // Asking again with the same package would change nothing: the
      // committer ignores a package it has seen.
      if (resend && this.offeredFor !== null) await this.rekey()
      wires.push(...this.offer(others))
    }
  }

  // A group of one tells the room about itself: I may be someone the others
  // still hold an old leaf of (a reload that found the room empty for a
  // moment). They swap me in, and I take that welcome.
  private announceGroupOfOne(others: string[], wires: Uint8Array[]) {
    if (!this.state || treeMembers(this.state).size !== 1 || !others.length) return
    const key = others.slice().sort().join(',')
    const hello = this.announcedFor === key ? null : this.hello()
    if (hello) {
      this.announcedFor = key
      wires.push(hello)
    }
  }

  private shouldYieldToLeader(lead: string | null): boolean {
    if (!this.state || lead === null) return false
    return lead < this.userId && treeMembers(this.state).size === 1
  }

  protected async syncNow(roster: readonly string[], resend: boolean): Promise<MediaStep> {
    if (this.closed) return this.step()
    const wires: Uint8Array[] = []
    const others = this.refreshRoom(roster)
    // A group is never given up because a lower id arrives: the newcomer
    // offers its package and is let in. Only a group of one, which nobody
    // else depends on, steps aside for a lower id, so that two members who
    // each found an empty room end up in one group.
    const lead = lowest(others)
    if (this.shouldYieldToLeader(lead)) {
      await this.leave()
    }
    await this.establishGroup(others, resend, wires)
    this.announceGroupOfOne(others, wires)
    if (this.canCommit(others)) {
      wires.push(...await this.commitRemoves(others))
      wires.push(...await this.commitAdds(others))
    }
    return this.step(wires)
  }

  /**
   * The group is out of step (a commit was missed): start over as a joiner
   * with a fresh key package, so the committer replaces my old leaf.
   */
  recover(roster: readonly string[]): Promise<MediaStep> {
    this.hint(roster)
    return this.enqueue(async () => {
      if (this.closed) return this.step()
      this.room = this.others(roster)
      return this.recoverNow()
    })
  }

  protected async recoverNow(): Promise<MediaStep> {
    if (!this.room.some(id => id < this.userId) || !this.resets.allow('self')) return this.step()
    await this.leave()
    return { wires: this.offer(this.room), epoch: null, key: null, verification: null }
  }

  /** Drops the group and the package it was joined with. */
  protected async leave() {
    this.discard()
    await this.rekey()
  }

  protected async rekey() {
    const old = this.own
    // Jobs run one at a time, so the group cannot be closed meanwhile.
    this.own = await generateKeyPackage(this.credential, defaultCapabilities(), defaultLifetime, [], this.cs)
    this.offeredFor = null
    wipeBytes(old.privatePackage.initPrivateKey)
    wipeBytes(old.privatePackage.hpkePrivateKey)
    wipeBytes(old.privatePackage.signaturePrivateKey)
  }

  protected hello(): Uint8Array | null {
    return encodeWire({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: this.own.publicPackage })
  }

  /** The key package for the room, unless this room already has it. */
  protected offer(others: readonly string[]): Uint8Array[] {
    const key = others.slice().sort().join(',')
    if (this.offeredFor === key) return []
    const wire = this.hello()
    if (!wire) return []
    this.offeredFor = key
    this.watchOffer()
    return [wire]
  }

  protected async createOwn(): Promise<Uint8Array[] | null> {
    const state = await createGroup(this.groupId, this.own.publicPackage, this.own.privatePackage, [], this.cs, clientConfig())
    if (!await this.adopt(state)) return null
    this.offeredFor = null
    return []
  }

  /**
   * Everyone else here offered a package and nobody is lower: nobody holds a
   * group, so the lowest of us founds one at once instead of waiting.
   */
  protected allOffered(others: readonly string[]): boolean {
    return !others.some(id => id < this.userId) && others.every(id => this.pending.has(id))
  }

  protected async foundNow(): Promise<MediaStep | null> {
    const created = await this.createOwn()
    if (!created) return null
    return this.step([...created, ...await this.commitAdds(this.room)])
  }
}
