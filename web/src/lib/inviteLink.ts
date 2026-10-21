// Invite links: /invite/{code} opens the sign-up screen with the code filled in
// and the community's name in the heading. The code is the one the server
// issues (letters, digits, "_" and "-", 6 to 64 characters); anything else is
// not treated as an invite.
const INVITE_PATH = /^\/invite\/([A-Za-z0-9_-]{6,64})\/?$/

/** The invite code in a path like /invite/abc123xyz, or '' when there is none. */
export function inviteCodeFromPath(pathname: string): string {
  return INVITE_PATH.exec(pathname)?.[1] ?? ''
}

/** True for the invite landing path, so it is never saved as a post-login route. */
export function isInvitePath(pathname: string): boolean {
  return inviteCodeFromPath(pathname) !== ''
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
}
