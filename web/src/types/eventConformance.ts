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

/** The events whose server shape is not assignable to the declared payload. */
type Mismatched = { [K in Checked]: ServerEventShapes[K] extends ServerEventPayloads[K] ? never : K }[Checked]

type None<T extends never> = T

/** Fails to compile, naming the events, when a server shape no longer fits. */
export type ServerShapesFitDeclaredPayloads = None<Mismatched>
