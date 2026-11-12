# Design note: a commit that adds a key package this member never saw (H-L2-04)

Code: `web/src/lib/media/groupBehind.ts`, hooks in `group.ts` (`validator`, `processCommit`). Rule text:
SECURITY.md, "What a member can do". Tests: `group.behind.test.ts`, the seed sweep and the production-ratio
run in `groupSim.settle.test.ts`.

## The defect

A member that reloads makes a second key package, under a new signature key. The committer (the lowest id)
answers it by swapping the member's old leaf in one commit (`[remove, add]`). Every receiver checks that
commit before it takes effect and refuses an add of a package the relay never delivered to it (`wasRelayed`,
`processAddProposal` in `groupVet.ts`). A relay that delivers the package to the committer and to some members
only therefore splits the room: the committer and those that got the package are in epoch N+1, the others
refuse the commit, stay in epoch N and are not told. The next commit starts them over (a "commit ahead"), but
that is a rotation away (20 to 32 minutes), and the other sign, two heartbeats of the lowest id for another
epoch, takes about 14 minutes at the lifetimes of production. Reproduced by simulator seed 149 (hostile relay,
production ratios): three of seven members without the group's key until the heartbeats find them.

## The rule, and the information each party uses

The rule that stays: a commit adds an identity only with a package that the relay delivered to this receiver
under that identity (`relayed`, per receiver, the last eight per member, pruned to the room).

| | Committer | Receiver |
|---|---|---|
| Decides on | its own `pending`/`accepted` record: the package arrived (any way) and is new under a new signature key | its own `relayed` record |
| Knows | what the relay delivered to it | what the relay delivered to it, and that the commit verified under the epoch it holds |
| Does not know | to whom else the relay delivered the package | to whom else the relay delivered it, and whether the committer has a newer epoch |

They differ exactly when the relay delivers selectively (or a reconnect loses a package: the honest relay
loses what was relayed while a socket was down). Neither side can tell the cases apart: a package may be on
its way, or never come.

## What the refusal protects against

A receiver may not take a leaf swap built on a package it never saw, because the identity in a basic credential
is not authenticated: a committer (or a relay in front of it) could otherwise add a leaf for an identity that
never asked for it, or put a leaf of a past tab (whose keys nobody holds any more) in place of a live member's.
The relay vouches for "this identity sent this package". Without the check a malicious lowest id could add keys
it chose for any listed identity (a second leaf for a present member, or a way back in for a removed one with
a package the member never made). The refusal is the receiver's own evidence and stays as it is.

## What an attacker gains or loses

- Hostile relay, today: one dropped package delivery (to every receiver but the committer) splits the room for
  the length of a rotation or about 14 minutes, at no further cost. It can also make a receiver start over by
  holding back a commit and delivering the next one ("commit ahead"), at the cost of one dropped message.
- Hostile relay, with the change: the same dropped delivery makes the receivers that did not get the package
  start over after one offer wait (4 s), at most once per reset limit (10 s, doubling to 60 s, as for every
  other start-over). The relay gains no new way to force a start-over: it needs a genuine commit of the
  committer for the receiver's own epoch that adds a package it holds back, one message dropped, the same price
  as a commit ahead. It loses the long split.
- Malicious member: it can send packages only under its own id, so it can cause the refusal only for its own
  leaf. A malicious lowest id could already take members out of the group, delay joins or stop the call; adding
  a package that some members never saw splits the group today, and with the change those members start over
  and come back into its group. It gains nothing it did not have, and the reset limit bounds how often it can
  do this to one receiver.
- Removed member: unchanged. The add rules, the removal rules and the committer rule are not touched; the
  start-over is the one the group already uses, and it comes back only with a fresh package that the committer
  has to add.

## Options

(a) Keep the refusal, take it as evidence of being behind: hold the commit for one offer wait, then start over.
No wire change, no capability: a pure receiver rule, invisible to old clients.

(b) Ask the committer for the package (a new message). A wire change and a capability bit; old committers
cannot answer. The answer would be a package the receiver then has to trust although it came through the same
party, and the committer would have to keep every package it ever used. More surface for the same result as a
start-over, which the group does anyway.

(c) The committer swaps only on a package it has evidence every member received. The only evidence available is
an acknowledgement (a new message again) or the receiver's silence, which a relay controls. It also makes the
committer wait for the slowest or a dead member, which a hung member could turn into a block of every swap
(a lowest-id availability attack). Rejected.

(d) Leave it. The finding's rating is S4 and the relay can already stop a call, but the cost to repair is small
and the same code path repairs the honest-relay case of a package lost to a reconnect.

Chosen: (a). It is the smallest change, it touches no rule a receiver applies to an add (`vetProposals`,
`wasRelayed`, `validator` are unchanged apart from recording why a commit was refused), it adds no message, and
it works against any committer, old ones included.

## The change

`validator` clears a flag, and `wasRelayed` sets it when the relay never delivered a package. Because
`scanCommitProposals` stops at the first failing proposal, the flag is set exactly when a commit of the
committer, verified against my tree, was refused for that reason and no other (not the committer rule, not a
removal, not the add identity). `processCommit` hands such a refusal to `refused`:

1. The first refusal for the epoch I hold keeps the commit and starts a timer of one offer wait.
2. When it fires, and my epoch is unchanged, the commit is taken again. If the package arrived meanwhile the
   same rules now accept it (no start-over: a package that was only slower than the commit).
3. If it is refused again for the same reason, I start over as for a commit ahead (`recoverNow`, bounded by the
   reset limit; while the limit holds me back the wait is repeated until it lets go).
4. Any commit taken, a start-over, or closing the group drops the held commit.

A second refusal of the same epoch during the wait (a repeated delivery) changes nothing, so a relay cannot
stretch the wait by repeating it.

## Properties

- A removed member cannot come back: the add and removal rules are unchanged. The start-over makes a new
  package that still needs the committer's add and the receivers' `wasRelayed`.
- A key package or a welcome is used once: the start-over generates a new package and wipes the old secrets
  (`leave`, `rekey`), as for any start-over; the held commit holds public bytes only.
- The vet rule receivers apply is not weaker: the retry runs the same validator; a refused commit is never
  taken, only reconsidered with what the relay delivered since.
- A hung or malicious lowest id cannot keep the Talk regrouping: the reset limit applies (10 s doubling to 60 s,
  quiet restarts it), and the group merges only downwards as before. The hold is per epoch and does not repeat.
- The relay cannot force start-overs more cheaply than today: see above. The simulator's invariants and the seed
  sweep are the check (numbers in the commit message).
- Mixed rooms: a new receiver starts over where a 3.0.9 one stays behind; a 3.0.9 receiver is unchanged. The
  committer is untouched, so no mix commits differently. Nobody is worse off than with 3.0.9.

## What it does not do

It does not prevent the refusal and does not make the committer's swap smarter: a stale package of a member
(for example one it offered earlier and has since replaced) that is delivered to the committer only still gets
that member's leaf swapped for a leaf nobody holds keys of. The receivers that never saw it now repair
themselves by starting over; the swapped member itself rejoins the same way. The time without the key is one
offer wait plus one start-over instead of a rotation.
