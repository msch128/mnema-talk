// Invite links: /invite/{code} opens the sign-up screen with the code filled in
// and the community's name in the heading. The code is the one the server
// issues (letters, digits, "_" and "-", 6 to 64 characters); anything else is
// not treated as an invite.
const INVITE_PATH = /^\/invite\/([A-Za-z0-9_-]{6,64})\/?$/
// The vanity link the Owner may set: /join/{slug}, 3 to 32 lowercase letters, digits and hyphens
// (read in any case; the server folds it).
const JOIN_PATH = /^\/join\/([A-Za-z0-9-]{3,32})\/?$/

/** The invite code in a path like /invite/abc123xyz, or '' when there is none. */
export function inviteCodeFromPath(pathname: string): string {
  return INVITE_PATH.exec(pathname)?.[1] ?? ''
}

/** The slug in a path like /join/meinclan, or '' when there is none. */
export function joinSlugFromPath(pathname: string): string {
  return JOIN_PATH.exec(pathname)?.[1]?.toLowerCase() ?? ''
}

/** True for the invite and vanity landing paths, so they are never saved as a post-login route. */
export function isInvitePath(pathname: string): boolean {
  return inviteCodeFromPath(pathname) !== '' || joinSlugFromPath(pathname) !== ''
}

/** The sign-up code of a vanity link: a "~" no real invite code can contain, then the slug. */
export function vanityInviteCode(slug: string): string {
  return `~${slug}`
}

/** The vanity link to share for a slug, relative to the page's origin. */
export function joinLink(origin: string, slug: string): string {
  return `${origin}/join/${encodeURIComponent(slug)}`
}

/** The link to share for a code, relative to the page's origin. */
export function inviteLink(origin: string, code: string): string {
  return `${origin}/invite/${encodeURIComponent(code)}`
}

/** What the sign-up screen knows about the invite it was opened with. */
export interface InviteContext {
  code: string
  /** The server's verdict; null when it could not be asked (rate limited, offline). */
  valid: boolean | null
  /** The community's name; empty when unknown or not set. */
  communityName: string
  /** When the code expires (RFC 3339); empty when it does not. */
  expiresAt: string
  /** True when the page was opened through a vanity link (/join/name); then code is '~name' or empty. */
  vanity?: boolean
}
