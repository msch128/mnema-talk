// Decisions Save makes in the roles editor, kept apart from its refs.
import type { Role } from '../types/domain'

export function saveClosed(role: Role | null, isBusy: boolean): boolean {
  if (!role) return true
  return isBusy
}

export function customNameMissing(role: Role, trimmed: string): boolean {
  if (role.is_everyone) return false
  return trimmed.length === 0
}

export function ownerUploadRejected(owner: boolean, upload: number | null | undefined): boolean {
  if (!owner) return false
  return upload === undefined
}

export function uploadPatch(owner: boolean, upload: number | null | undefined, current: number | null) {
  // Only the Owner may send the upload limit; it is the one field the
  // server refuses from role managers.
  if (!owner) return {}
  if (upload === undefined || upload === current) return {}
  return { max_upload_mb: upload }
}
