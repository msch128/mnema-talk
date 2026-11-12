// A welcome from a lower id outside my group (SECURITY.md, "Voice and screenshare"): it replaces the group I hold
// only if it opens with the key package I hold and passes the committer rule; one that does not open changes
// nothing and does not count (the wait between two that do, a minute doubling to ten, is the `rivals` back-off of
// groupBase.ts: pinned in groupConstants.test.ts). Tested by who is in the group afterwards: the existing test of this
// (group.recovery.test.ts, "a rival welcome") compares epoch numbers, and the group that replaces the old one
// is at epoch 1 as well, so it cannot tell a replaced group from a kept one.
import { webcrypto } from 'node:crypto'
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import * as mls from 'ts-mls'
import type { Credential, KeyPackage } from 'ts-mls'
import { fixtureId } from '../../test-fixtures.fixture'
import { mediaGroupId, mediaUserId } from './bounds'
import { MediaGroup } from './group'

const previous = globalThis.crypto
const aliceId = fixtureId(1)
const carolId = fixtureId(3)
const daveId = fixtureId(4)
const strangerId = fixtureId(2)
const channelId = fixtureId(9)
const channel = mediaGroupId(channelId) as Uint8Array

beforeAll(() => { vi.stubGlobal('crypto', webcrypto) })
afterAll(() => { vi.stubGlobal('crypto', previous) })

async function opened(userId: string) {
  const group = await MediaGroup.open(userId, channelId)
  if (!group) throw new Error('expected a media group')
  return group
}

function wireAt(wires: Uint8Array[], index: number): Uint8Array {
  const wire = wires[index]
  if (!wire) throw new Error('expected a handshake wire')
  return wire
}

async function welcomeFrom(signerId: string, joiner: Uint8Array): Promise<Uint8Array> {
  const cs = await mls.getCiphersuiteImpl(mls.getCiphersuiteFromName('MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519'))
  const own = await mls.generateKeyPackage({ credentialType: 'basic', identity: new TextEncoder().encode(signerId) }, mls.defaultCapabilities(), mls.defaultLifetime, [], cs)
  const state = await mls.createGroup(channel, own.publicPackage, own.privatePackage, [], cs, {
    keyRetentionConfig: mls.defaultKeyRetentionConfig,
    lifetimeConfig: mls.defaultLifetimeConfig,
    keyPackageEqualityConfig: mls.defaultKeyPackageEqualityConfig,
    paddingConfig: mls.defaultPaddingConfig,
    authService: {
      async validateCredential(credential: Credential) {
        return credential.credentialType === 'basic' && mediaUserId(new TextDecoder().decode(credential.identity)) === new TextDecoder().decode(credential.identity)
      },
    },
  })
  const message = mls.decodeMlsMessage(joiner, 0)?.[0]
  if (!message || message.wireformat !== 'mls_key_package') throw new Error('expected a key package')
  const committed = await mls.createCommit({ state, cipherSuite: cs }, { extraProposals: [{ proposalType: 'add', add: { keyPackage: message.keyPackage } }], wireAsPublicMessage: true, ratchetTreeExtension: true })
  if (!committed.welcome) throw new Error('expected a welcome')
  return mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_welcome', welcome: committed.welcome })
}

const ownPackage = (group: MediaGroup) => mls.encodeMlsMessage({ version: 'mls10', wireformat: 'mls_key_package', keyPackage: (group as unknown as { own: { publicPackage: KeyPackage } }).own.publicPackage })

// With a roster, a receiver also syncs to it after each wire: the call does the same, since the roster arrives
// before the handshake that depends on it.
async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[], roster: string[]) {
  const pending = wires.map(wire => ({ from, wire }))
  for (const [id, group] of groups) if (roster.includes(id)) group.hint(roster)
  while (pending.length) {
    const item = pending.shift()
    if (!item) break
    for (const [id, group] of groups) {
      if (id === item.from) continue
      const step = await group.receive(item.from, item.wire)
      for (const wire of step.wires) pending.push({ from: id, wire })
      const synced = await group.sync(roster)
      for (const wire of synced.wires) pending.push({ from: id, wire })
    }
  }
}

/** Carol and Dave in a group of two; Alice, who is lower than both, is in the Talk but not in that group. */
async function carolAndDave() {
  const carol = await opened(carolId)
  const dave = await opened(daveId)
  const groups = new Map([[carolId, carol], [daveId, dave]])
  await carol.sync([carolId])
  await deliver(groups, daveId, (await dave.sync([carolId, daveId])).wires, [carolId, daveId])
  await carol.sync([carolId, daveId])
  await dave.sync([carolId, daveId])
  return { carol, dave, roster: [aliceId, carolId, daveId] }
}

describe('a welcome from a lower id outside my group', () => {
  it('leaves the epoch number as it was, so an assertion on the number cannot tell a replaced group from a kept one', async () => {
    const { carol, dave, roster } = await carolAndDave()
    const before = await carol.sync(roster)
    const joined = await carol.receive(aliceId, await welcomeFrom(aliceId, ownPackage(carol)))
    expect(joined.epoch).toBe(before.epoch)
    expect(joined.verification?.members).not.toEqual(before.verification?.members)
    expect(joined.verification?.code).not.toBe(before.verification?.code)
    for (const group of [carol, dave]) await group.close()
  }, 30_000)

  it('that does not open with my package changes nothing and spends nothing: the next one that does replaces my group at once', async () => {
    const { carol, dave, roster } = await carolAndDave()
    const before = await carol.sync(roster)
    expect(before.verification?.members).toEqual([carolId, daveId])
    const stranger = await opened(strangerId)
    const bogus = await welcomeFrom(aliceId, wireAt((await stranger.sync([aliceId, strangerId])).wires, 0))
    expect((await carol.receive(aliceId, bogus)).verification?.members).toEqual([carolId, daveId])
    const real = await welcomeFrom(aliceId, ownPackage(carol))
    const joined = await carol.receive(aliceId, real)
    expect(joined.verification?.members).toEqual([aliceId, carolId])
    for (const group of [carol, dave, stranger]) await group.close()
  }, 30_000)

  it('is refused from a sender that is not lower than the member who commits in my group', async () => {
    const { carol, dave } = await carolAndDave()
    const roster = [carolId, daveId, fixtureId(5)]
    await carol.sync(roster)
    const real = await welcomeFrom(fixtureId(5), ownPackage(carol))
    expect((await carol.receive(fixtureId(5), real)).verification?.members).toEqual([carolId, daveId])
    for (const group of [carol, dave]) await group.close()
  }, 30_000)
})
