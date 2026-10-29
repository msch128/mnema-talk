import { createCommit, type CiphersuiteImpl, type ClientState } from 'ts-mls'
import type { AddProposal } from './groupEpochCommit'

/** Tries each package alone: the ones ts-mls accepts, with their trial states to wipe, and the ones it refuses (signature, lifetime, suite, duplicate key). */
export async function vetAdds(state: ClientState, cs: CiphersuiteImpl, proposals: readonly AddProposal[]) {
  const usable: AddProposal[] = []
  const dropped: AddProposal[] = []
  const trials: ClientState[] = []
  for (const proposal of proposals) {
    try {
      trials.push((await createCommit({ state, cipherSuite: cs }, { extraProposals: [proposal] })).newState)
      usable.push(proposal)
    } catch {
      dropped.push(proposal)
    }
  }
  return { usable, dropped, trials }
}
