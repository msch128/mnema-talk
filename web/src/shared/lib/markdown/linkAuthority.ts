// Which authorities of an http(s) link the client links at all: none with a
// user name or password (https://allowed.example@other.example names one host
// and opens another) and none with a control or invisible format character
// (they hide where the host starts or ends). The server's automod judges such
// links on its own (internal/automod/linkscan.go), also for clients with an
// older bundle; this only spares readers the deception.

const AUTHORITY = /^https?:\/\/([^/?#\\]*)/i
const HIDDEN = /[\p{Cc}\p{Cf}]/u

/** Whether the authority of an http(s) URL is free of user info and hidden characters. */
export function plainAuthority(raw: string): boolean {
  const authority = AUTHORITY.exec(raw)?.[1] ?? ''
  return !authority.includes('@') && !HIDDEN.test(authority)
}
