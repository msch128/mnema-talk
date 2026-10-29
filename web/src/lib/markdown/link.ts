// Which link targets may be rendered, and when opening one needs a
// confirmation because the visible text says something else.

// eslint-disable-next-line no-control-regex
const SAFE_HREF = /^(?:https?:\/\/(?!\/)|mailto:)[^\s<>"`\\\u0000-\u001f\u007f]+$/i

/** The URL when it is http(s) or mailto and parses; otherwise null. */
export function safeHref(raw: string): string | null {
  if (!SAFE_HREF.test(raw)) return null
  try {
    const url = new URL(raw)
    if (url.protocol === 'mailto:') return url.pathname ? raw : null
    return url.hostname ? raw : null
  } catch {
    return null
  }
}

/** The host a link leads to, as the browser reads it ("example.com"). */
export function hrefHost(href: string): string {
  try {
    const url = new URL(href)
    return url.protocol === 'mailto:' ? decodeURIComponent(url.pathname) : url.hostname
  } catch {
    return ''
  }
}

const HOST_LIKE = /^(?:https?:\/\/)?([a-z0-9-]+(?:\.[a-z0-9-]+)+)(?::\d+)?(?:[/?#]|$)/i

function bareHost(text: string): string {
  const match = HOST_LIKE.exec(text.trim())
  return match?.[1]?.toLowerCase().replace(/^www\./, '') ?? ''
}

/**
 * Whether opening [label](href) needs a confirmation: yes unless the label
 * itself names the destination (a URL or host on the same host, or the mail
 * address of a mailto: link).
 */
export function needsConfirm(label: string, href: string): boolean {
  const host = hrefHost(href).toLowerCase().replace(/^www\./, '')
  if (!host) return true
  if (href.toLowerCase().startsWith('mailto:')) return label.trim().toLowerCase() !== host
  return bareHost(label) !== host
}
