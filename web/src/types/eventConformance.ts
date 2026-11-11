// Compile-time only. eventShapes.ts is generated from the server's Go event
// types; events.ts declares what the web app accepts for each event (with the
// runtime checks next to it). Everything the server publishes must fit the
// declared payload, so a Go event type that changes its JSON fails the type
// check here until events.ts follows.
import type { ServerEventPayloads } from './events'
import type { ServerEventShapes } from './eventShapes'

/**
 * Not compared, because Go has no type for what the web app narrows:
 * presence_snapshot maps to plain strings in Go (and an older server sent a
 * list), and the type of webrtc_offer is pion's SDP type, always "offer".
 */
type NotCompared = 'presence_snapshot' | 'webrtc_offer'

type Checked = Exclude<keyof ServerEventShapes & keyof ServerEventPayloads, NotCompared>

/** The field names of an object shape; nothing for null, arrays and plain values. */
type Fields<T> = T extends readonly unknown[] ? never : T extends object ? keyof T : never

/** S when no single member of the declared payload D takes it without a field left over. */
type Unfit<S, D> = S extends unknown ? ([FitsOne<S, D>] extends [never] ? S : never) : never
type FitsOne<S, D> = D extends unknown ? (S extends D ? ([Exclude<Fields<S>, Fields<D>>] extends [never] ? true : never) : never) : never

/**
 * The events with a server shape that fits no member of the declared payload.
 * Plain assignability is not enough: {channel_id, refresh} is assignable to
 * {refresh}, yet the runtime check of that member refuses the extra field. So
 * each server shape must fit one declared member that also names all of its
 * fields.
 */
type Mismatched = { [K in Checked]: [Unfit<ServerEventShapes[K], ServerEventPayloads[K]>] extends [never] ? never : K }[Checked]

type None<T extends never> = T

/** Fails to compile, naming the events, when a server shape no longer fits. */
export type ServerShapesFitDeclaredPayloads = None<Mismatched>
