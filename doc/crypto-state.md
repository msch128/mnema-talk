# Cryptographic state and outbox persistence

Status: implemented persistence foundation for the E2EE integration. It does not
encrypt messages, authenticate devices, provide recovery, or make the current
application end-to-end encrypted. No existing chat or media path uses this module
yet. The production cryptographic provider and its admission/freshness contract
remain release gates.

The implementation is `web/src/lib/crypto/stateStore.js`. Runtime dependencies
are browser IndexedDB and Web Locks. `fake-indexeddb` 6.2.5 is an exact-pinned
development dependency for transaction tests and is not shipped with the app.

## Decision and invariants

A community/account/device tuple identifies one store and one exclusive native
Web Lock. Identifiers must come from the trusted device configuration and use
opaque IDs, rather than names, email addresses or credentials. JSON tuple
encoding avoids delimiter collisions. A second tab receives an explicit
ownership conflict. The implementation never steals locks, falls back to a
`localStorage` lease, or silently enables an unsafe single-tab mode. Native
ownership is coordination between cooperative same-origin clients, not a defense
against malicious scripts running on that origin. The Web Locks API defines the
exclusive lock lifetime around the returned callback promise.
[Web Locks specification](https://w3c.github.io/web-locks/#dom-lockmanager-request)

Each group has a versioned opaque provider state, context ID and monotonic local
revision. An outgoing event contains the caller's stable event ID, context ID,
resulting revision and ciphertext. A single IndexedDB transaction writes the new
provider state, the exact outgoing ciphertext, and aggregate usage counters.
`commitSend` resolves only after the transaction completes; successful individual
write requests do not authorize transport. Writes request strict durability and
reject a browser which cannot expose that requested mode. The mode is a browser
durability hint, not a guarantee against disk corruption, power loss or snapshot
restoration. IndexedDB transaction completion and durability are distinct from
the application sending an event.
[IndexedDB specification](https://w3c.github.io/IndexedDB/#transaction-durability-hint)

The application sends the returned event only after `commitSend` resolves. After
a transport failure or missing response it obtains the already committed event
through `listPending` and sends those exact bytes again. It must never recreate
the ciphertext from a restored counter or re-encrypt the same event to retry it.
The server needs a durable idempotency contract for the scoped event ID and may
acknowledge only after durable acceptance. `acknowledge` removes only the exact
stored event with the expected context; it never rolls back provider state.
The application must never recycle event IDs, including after acknowledgement.
The adapter rejects overwriting pending events; permanent server-side event
deduplication is a separate requirement.

Every cryptographic state change uses compare-and-swap on the stored revision.
Concurrent sends prepared from the same state cannot both advance it. Receive
state changes use `commitState` with the same boundary. Failed transactions,
malformed storage, quota rejection, invalid send inputs and uncertain write
results invalidate local readiness. The cryptographic service must then discard
its corresponding in-memory provider state; it cannot keep encrypting from a
state which may disagree with persistence. Closing or upgrading the store aborts
in-flight transactions, cancels establishment, clears readiness and releases
ownership only after queued work settles.

## Restoring a snapshot cannot restore live readiness

Readiness is intentionally memory-only. Every new store owner, including one
following a renderer crash, starts with all groups blocked for new sends.
It may read opaque state and already committed outbox entries. It must establish
a fresh cryptographically authorized context before advancing sender state.

A locally saved revision, timestamp, checksum or counter cannot prove that the
whole browser database has not been restored to an older snapshot. Both the
state and such markers can roll back together. Therefore this adapter does not
claim to detect snapshot rollback. It treats all reopenings as uncertain and
requires the trusted provider's freshness boundary.

The required `establishFreshContext` callback receives the namespace, group ID,
previous stored state and an AbortSignal. It must return `{ contextId, state }`
only after establishing genuinely fresh, authorized protocol state independently
of the restored local snapshot. A new string, a backend assertion or a random
room password is insufficient. The selected MLS/media integration must define
how this is achieved, how other members authorize it and how group forks are
handled. Reusing the immediate stored context ID is rejected, but this simple
check cannot prove that cryptographic keys or sender counters are fresh.

Establishment happens outside IndexedDB transactions. It has a maximum of 30
seconds, configurable downward, and is cancelled when the store closes. A late
callback result is discarded. The provider must respect cancellation and dispose
of unused cryptographic sessions. A failure after the provider returned but
before persistence completes also requires the calling service to discard that
session. No key material or ciphertext is logged by the adapter's error messages.

## API contract

| Operation | Contract |
| --- | --- |
| `openCryptoStateStore(options)` | Acquires exclusive device ownership and opens schema version 1. Requires a trusted fresh-context provider. Unsupported storage/ownership fails closed. |
| `loadState(groupId)` | Returns a cloned opaque stored group record, or `undefined`. Reading does not authorize sending. |
| `establishContext({groupId, expectedRevision})` | Calls the trusted provider, then atomically persists the fresh context using compare-and-swap. New groups start at expected revision 0. |
| `commitSend({groupId, contextId, expectedRevision, eventId, nextState, ciphertext})` | Copies bounded byte buffers immediately, then atomically saves state and ciphertext. Returns the committed outbox event. |
| `commitState({groupId, contextId, expectedRevision, nextState})` | Persists a receive/control state transition; requires the active authorized context. |
| `listPending(groupId, limit)` | Reads a bounded batch of immutable ciphertext records. Entries are in event-ID key order; this is not a guaranteed delivery or protocol order. |
| `acknowledge({groupId, eventId, contextId})` | Removes a durably accepted event and updates quota usage. Repeated acknowledgement is harmless. |
| `close()` | Cancels establishment and pending transactions, closes the database and releases device ownership. Idempotent. |

The protocol/transport coordinator owns ordering and batch selection. For
protocols requiring strict order, use the stored resulting revision rather than
assuming lexicographic event IDs represent chronological order. A new epoch or
device revocation can change whether retrying old pending events is allowed;
retrieving ciphertext alone is not a membership authorization decision.

The namespace and byte budgets are fixed for the store's lifetime. Byte inputs
must be non-empty `Uint8Array` values. Defaults bound each state to 8 MiB, all
states to 16 MiB, each ciphertext to 1 MiB, the outbox to 8 MiB/256 events,
group count to 256 and outstanding operations to 32. Callers may lower limits,
but cannot raise them above these defaults. Queue overload rejects new work;
it never silently drops a previously committed event. Reducing limits below
the existing stored usage blocks writes until a separately reviewed migration
or cleanup resolves the conflict.

## Local protection and remaining integration gates

Opaque state may contain secret material. This adapter does not encrypt it at
rest or guarantee secure memory erasure. The reviewed provider must define the
appropriate protected serialization and device-key storage contract. It must
not serialize plaintext messages into the state or attach debug key fields to
events. Browser/OS compromise and malicious same-origin client code remain
outside this storage boundary. Origin trust and independently delivered client
code remain product requirements.

The current experimental OpenMLS browser binding has an in-memory provider and
does not expose a reviewed transactional storage adapter. Wiring it to this
module requires a production wrapper covering all provider records, atomic
serialization, bounded decoding, device credentials, authorized freshness,
epoch ordering and cancellation. Saving only the visible group object while
discarding provider keystore state is insufficient. This module cannot repair
an incorrect crypto provider or substitute for independent protocol review.

Chat/SFU integration, protected attachments, media provider conformance,
revocation, recovery/history policy, independent client delivery and encrypted
100/250-user measurements remain separate gates. This foundation provides no
capacity or latency promise for those paths.

## Verification

Run the unit transaction suite from `web/`:

```sh
npx vitest run src/lib/crypto/stateStore.test.js
```

Run native Chromium tests from `e2e/`, using the existing Playwright installation:

```sh
npx playwright test tests/crypto-state.spec.js
```

The unit suite forces abort after the group write and before outbox insertion;
checks immutable retry bytes, context/revision conflicts, quotas, cancellation,
future schema rejection, malformed state and an externally restored consistent
older snapshot. The native tests use real IndexedDB and same-origin Web Locks,
crash the owning renderer through CDP, then demonstrate that the replacement tab
can retry saved ciphertext but cannot resume the old sending context. A second
native test verifies atomic rollback between the state and outbox writes.

Synthetic provider states in these tests isolate the persistence contract. They
are deliberately not presented as MLS, SFrame, device authorization or nonce
safety proofs. `fake-indexeddb` is an in-memory implementation and cannot prove
disk durability; Chromium execution adds browser lifecycle coverage, not a
physical power-loss guarantee.
[fake-indexeddb documentation](https://github.com/dumbmatter/fakeIndexedDB)
