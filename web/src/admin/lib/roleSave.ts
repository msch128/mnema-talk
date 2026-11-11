// What Save sends for the roles editor, kept apart from its draft.

export function uploadPatch(owner: boolean, upload: number | null | undefined, current: number | null) {
  // Only the Owner may send the upload limit; it is the one field the
  // server refuses from role managers.
  if (!owner) return {}
  if (upload === undefined || upload === current) return {}
  return { max_upload_mb: upload }
}
