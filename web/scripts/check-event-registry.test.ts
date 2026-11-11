import { test } from 'node:test'
import assert from 'node:assert/strict'
import { differences, interfaceKeys, shapeDifferences } from './check-event-registry.ts'

const source = `
export interface Other {
  not_an_event: string
}
export interface ServerEventPayloads {
  pong: { t: number }
  /** A comment. */
  message_create: Message
}
`

test('interfaceKeys lists the top-level keys of the named interface only', () => {
  assert.deepEqual(interfaceKeys(source, 'ServerEventPayloads'), ['pong', 'message_create'])
  assert.throws(() => interfaceKeys(source, 'Missing'), /no interface Missing/)
})

test('differences names events missing on either side', () => {
  assert.deepEqual(differences('server', ['pong', 'typing'], ['pong']), ['server event typing is in the Go registry but not in events.ts'])
  assert.deepEqual(differences('client', ['ping'], ['ping', 'extra']), ['client event extra is in events.ts but not in the Go registry'])
  assert.deepEqual(differences('server', ['pong'], ['pong']), [])
})

test('shapeDifferences names events without a generated shape and shapes without an event', () => {
  assert.deepEqual(shapeDifferences(['pong', 'typing'], ['pong']), ['server event typing has no shape in eventShapes.ts (no Go event type is registered for it)'])
  assert.deepEqual(shapeDifferences(['pong'], ['pong', 'extra']), ['eventShapes.ts has a shape for extra, which is not in the Go registry'])
  assert.deepEqual(shapeDifferences(['pong'], ['pong']), [])
})
