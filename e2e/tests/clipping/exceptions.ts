import type { ClipFinding, ClipRule } from './auditor'

// Deliberate clipping the sweep accepts. Every entry names the scene (or '*'),
// the rule and the element, and says why it is not a defect; an entry without
// a reason fails the sweep. Keep this list empty unless a product decision is
// written next to the entry.

export interface ClipException {
  scene: string
  rule: ClipRule
  /** The finding's selector, exactly as the auditor reports it. */
  selector: string
  reason: string
}

export const EXCEPTIONS: ClipException[] = []

export function isExcepted(scene: string, finding: ClipFinding, list: ClipException[] = EXCEPTIONS): boolean {
  return list.some(e => (e.scene === '*' || e.scene === scene) && e.rule === finding.rule && e.selector === finding.selector)
}

/** Entries without a written reason, which the sweep refuses. */
export function unexplained(list: ClipException[] = EXCEPTIONS): ClipException[] {
  return list.filter(e => e.reason.trim().length < 20)
}
