import type { LinkPreview } from '../types/domain'

// The player a link card may load after a click. The server builds the embed
// address from the video id of the link for two providers; this file checks the
// same shapes again, so a malformed or tampered answer can never put another
// address into an iframe. The CSP's frame-src allows exactly these two hosts
// (internal/httpx/middleware.go) and the iframe is sandboxed.

const YOUTUBE = /^https:\/\/www\.youtube-nocookie\.com\/embed\/[A-Za-z0-9_-]{11}(\?start=\d{1,5})?$/
const VIMEO = /^https:\/\/player\.vimeo\.com\/video\/\d{1,12}\?dnt=1(&h=[0-9a-f]{6,32})?$/

export const PLAYER_SANDBOX = 'allow-scripts allow-same-origin allow-presentation allow-popups allow-popups-to-escape-sandbox'
export const PLAYER_ALLOW = 'autoplay; encrypted-media; picture-in-picture; fullscreen'

/** The iframe address (autoplay on, since the member just pressed play), or null if the embed is not an allowed player. */
export function playerSrc(embed: LinkPreview['embed']): string | null {
  if (!embed) return null
  const ok = embed.provider === 'youtube' ? YOUTUBE.test(embed.url) : embed.provider === 'vimeo' ? VIMEO.test(embed.url) : false
  if (!ok) return null
  return embed.url + (embed.url.includes('?') ? '&' : '?') + 'autoplay=1'
}

/** raw when it is an http(s) URL, else ''. For hrefs that come from a fetched page. */
export function safeHttpUrl(raw: string | undefined): string {
  if (!raw) return ''
  try {
    const u = new URL(raw)
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.href : ''
  } catch {
    return ''
  }
}

/** A #rrggbb colour, else ''. */
export function safeColor(raw: string | undefined): string {
  return raw && /^#[0-9a-f]{6}$/.test(raw) ? raw : ''
}
