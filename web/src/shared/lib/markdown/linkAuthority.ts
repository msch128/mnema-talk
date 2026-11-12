// Which authorities of an http(s) link the client links at all: none with a
// user name or password (https://allowed.example@other.example names one host
// and opens another) and none with a control or invisible format character
// (they hide where the host starts or ends), also not one written as a percent
// escape (%E2%80%8B), which the URL parser decodes before it reads the host.
// The server's automod judges such links on its own
// (internal/automod/linkscan.go), also for clients with an older bundle; this
// only spares readers the deception.

const AUTHORITY = /^https?:\/\/([^/?#\\]*)/i
const HIDDEN = /[\p{Cc}\p{Cf}]/u

/** The authority with its percent escapes decoded, or null when they are no UTF-8. */
function decoded(authority: string): string | null {
  try {
    return decodeURIComponent(authority)
  } catch {
    return null
  }
}

/** Whether the authority of an http(s) URL is free of user info and hidden characters, as written and as the URL parser decodes it. */
export function plainAuthority(raw: string): boolean {
  const authority = AUTHORITY.exec(raw)?.[1] ?? ''
  let url: URL
  try {
    url = new URL(raw)
  } catch {
    return false
  }
  if (authority.includes('@') || url.username !== '' || url.password !== '') return false
  const text = decoded(authority)
  return text !== null && ![authority, text, url.hostname].some(part => HIDDEN.test(part))
}
