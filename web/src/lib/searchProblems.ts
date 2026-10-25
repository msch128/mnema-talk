// Why a query cannot be searched: filters whose value matches nothing. A
// problem is only reported once the token is finished (followed by a space);
// while it is still being typed the search just waits.

import type { FilterKey, SearchFilters } from './searchQuery'
import type { ClosedFilterToken } from './searchQuery'
import { AUTHOR_KINDS, HAS_KINDS, IS_KINDS, filterTokens } from './searchQuery'

export type SearchProblem =
  | { kind: 'member' | 'channel'; filter: FilterKey; value: string }
  | { kind: 'date'; filter: FilterKey; value: string }
  | { kind: 'option'; filter: FilterKey; value: string; options: string }
  | { kind: 'empty'; filter: FilterKey; value: string }

export interface SearchDiagnosis {
  problems: SearchProblem[]
  /** A filter token is still being typed and does not (yet) fit: keep what is shown. */
  pending: boolean
}

const OPTIONS: Partial<Record<FilterKey, readonly string[]>> = { has: HAS_KINDS, is: IS_KINDS, author: AUTHOR_KINDS }

function invalidProblem(key: FilterKey, value: string): SearchProblem {
  if (!value) return { kind: 'empty', filter: key, value }
  const options = OPTIONS[key]
  if (options) return { kind: 'option', filter: key, value, options: options.join(', ') }
  return { kind: 'date', filter: key, value }
}

/** The problem of one filter token: null when it is fine, 'pending' while it is still being typed. */
function tokenProblem(t: ClosedFilterToken, filters: SearchFilters, unresolved: FilterKey[]): SearchProblem | 'pending' | null {
  let problem: SearchProblem | null = null
  if (!t.valid) problem = invalidProblem(t.key, t.value)
  else if (unresolved.includes(t.key) && filters[t.key] === t.value) problem = { kind: t.key === 'in' ? 'channel' : 'member', filter: t.key, value: t.value }
  if (problem && t.open) return 'pending'
  return problem
}

/** Reads the line against the filters that were resolved to ids (unresolved holds the others). */
export function diagnoseSearch(input: string, filters: SearchFilters, unresolved: FilterKey[]): SearchDiagnosis {
  const problems: SearchProblem[] = []
  let pending = false
  for (const t of filterTokens(input)) {
    const found = tokenProblem(t, filters, unresolved)
    if (found === 'pending') pending = true
    else if (found) problems.push(found)
  }
  return { problems, pending }
}
