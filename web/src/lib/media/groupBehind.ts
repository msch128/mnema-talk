import type { ClientState, KeyPackage, PublicMessage } from 'ts-mls'
import { OFFER_WAIT_MS, type MediaStep } from './groupParams'
import { treeMembers } from './groupTree'
import { GroupMerge } from './groupMerge'

/**
 * The commit is the committer's, it verified against my tree, and the rules
 * refused it only because it adds a key package that the relay never delivered
 * to me. Others were given that package, so they may have taken the commit and
 * be one epoch ahead. I wait one offer wait for the package (the relay may
 * have it on its way: the commit is then taken as it stands) and start over if
 * the commit is still refused for that reason. See SECURITY.md, "A commit that
 * adds a package I never saw".
 */
interface Held {
  state: ClientState
  from: string
  message: PublicMessage
  timer: ReturnType<typeof setTimeout> | null
  due: boolean
}

export abstract class GroupBehind extends GroupMerge {
  /** The rules refused the commit in hand because of an add of a package never relayed to this member. */
  private unseenAdd = false
  private held: Held | null = null

  protected abstract receivePublic(from: string, publicMessage: PublicMessage, members: Map<string, number> | null): Promise<MediaStep>

  protected override wasRelayed(id: string, keyPackage: KeyPackage): boolean {
    const seen = super.wasRelayed(id, keyPackage)
    if (!seen) this.unseenAdd = true
    return seen
  }

  /** A commit is about to be vetted: what the last one left says nothing about it. */
  protected clearUnseen() {
    this.unseenAdd = false
  }

  /** The rules refused `message`, a commit of this member's epoch. Starts over if that is the sign of being behind. */
  protected async refused(from: string, message: PublicMessage): Promise<MediaStep> {
    if (!this.unseenAdd || !this.state) return this.step()
    this.unseenAdd = false
    const held = this.held
    if (!held || held.state !== this.state) return this.hold(from, message, this.state)
    // Still the same commit after the wait, or another one in the same epoch: either way nothing was taken since.
    if (!held.due) return this.step()
    // The reset limit holds me back (a start-over a moment ago): ask again when it lets go.
    const wait = this.resets.waitMs('self')
    if (wait > 0) return this.arm(held, wait + 50)
    this.release()
    return this.recoverNow()
  }

  override close(): Promise<void> {
    this.release()
    return super.close()
  }

  private release() {
    if (this.held?.timer) clearTimeout(this.held.timer)
    this.held = null
  }

  private hold(from: string, message: PublicMessage, state: ClientState): MediaStep {
    this.release()
    return this.arm({ state, from, message, timer: null, due: false }, OFFER_WAIT_MS)
  }

  private arm(held: Held, ms: number): MediaStep {
    held.due = false
    held.timer = setTimeout(() => {
      void this.enqueue(async () => (this.closed ? null : this.retry(held))).then(step => step && this.stepListener(step))
    }, ms)
    this.held = held
    return this.step()
  }

  /** The wait is over: take the commit again, now with whatever came meanwhile. It is refused again for the same reason, or taken. */
  private async retry(held: Held): Promise<MediaStep | null> {
    if (this.held !== held) return null
    if (this.state !== held.state) {
      this.release()
      return null
    }
    held.due = true
    const step = await this.receivePublic(held.from, held.message, treeMembers(held.state))
    if (this.held === held && held.due) this.release()
    return step
  }
}
