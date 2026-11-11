// What the wiki's refusals mean, by their stable code and not by their wording.
import { isApiError } from '../../shared/lib/api'

/** The code of a save the server refused because the page changed after it was read. */
export const STALE_REVISION = 'STALE_REVISION'

/** True when the server refused because somebody saved the page meanwhile. */
export function isStaleRevision(error: unknown): boolean {
  return isApiError(error) && error.code === STALE_REVISION
}
