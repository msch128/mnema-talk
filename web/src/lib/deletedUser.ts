import { t } from '../i18n'

// A deleted account stays as a tombstone whose username starts with this (the
// server reserves the prefix, so nobody can pick it).
const DELETED_PREFIX = 'deleted-'

/** True for the placeholder name of a deleted account. */
export function isDeletedUsername(username: string | undefined): boolean {
  return typeof username === 'string' && username.startsWith(DELETED_PREFIX)
}

/** The name to show for an author: "Deleted user" in the member's language for a deleted account. */
export function shownName(user: { display_name?: string | undefined; username?: string | undefined }): string {
  if (isDeletedUsername(user.username)) return t('user.deleted')
  return user.display_name || user.username || ''
}
