/**
 * The live normalisation of the sign-up form: spaces become "_" and capitals
 * lower-case, so the person sees the name the server will store. Login does
 * not use it; accounts that predate the rules keep names with capitals or "-".
 */
export function normalizeUsername(raw: string): string {
  return raw.replace(/ /g, '_').toLowerCase()
}
