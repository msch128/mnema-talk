// Hands handshakes between media groups in one process, as the relay would, until nothing is left to send.
import type { MediaGroup } from './group'

interface Pending { from: string, wire: Uint8Array }

interface Delivery {
  groups: Map<string, MediaGroup>
  roster: string[] | undefined
  tally: { wires: number } | undefined
  pending: Pending[]
}

function queue(delivery: Delivery, from: string, wires: Uint8Array[]) {
  for (const wire of wires) delivery.pending.push({ from, wire })
  if (delivery.tally) delivery.tally.wires += wires.length
}

/** A wire seen once from a sender is not handed on again (a copy would only be refused). */
function mark(item: Pending): string {
  return `${item.from}:${item.wire.byteLength}:${item.wire[0]}:${item.wire[item.wire.byteLength - 1]}`
}

async function receiveAt(delivery: Delivery, id: string, group: MediaGroup, item: Pending) {
  queue(delivery, id, (await group.receive(item.from, item.wire)).wires)
  if (delivery.roster?.includes(id)) queue(delivery, id, (await group.sync(delivery.roster)).wires)
}

async function fanOut(delivery: Delivery, item: Pending) {
  for (const [id, group] of delivery.groups) {
    if (id !== item.from) await receiveAt(delivery, id, group, item)
  }
}

function hintAll(groups: Map<string, MediaGroup>, roster: string[]) {
  for (const [id, group] of groups) if (roster.includes(id)) group.hint(roster)
}

// With a roster, a receiver also syncs to it after each wire: the call does the
// same, since the roster arrives before the handshake that depends on it.
export async function deliver(groups: Map<string, MediaGroup>, from: string, wires: Uint8Array[], roster?: string[], tally?: { wires: number }) {
  const delivery: Delivery = { groups, roster, tally, pending: [] }
  if (roster) hintAll(groups, roster)
  queue(delivery, from, wires)
  const seen = new Set<string>()
  for (let item = delivery.pending.shift(); item; item = delivery.pending.shift()) {
    const key = mark(item)
    if (seen.has(key)) continue
    seen.add(key)
    await fanOut(delivery, item)
  }
}
