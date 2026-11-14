// The address a link meant for other people starts with. A desktop client is
// served from its own local origin (https://mnema.localhost), which nobody
// else can open, so shared links use the address the server announces in
// GET /api/health (its PUBLIC_URL). Only a server that announces none (a dev
// server, an older release) falls back to the origin of the page.

/** The origin of `publicUrl` when it is an http(s) address, else ''. */
function announcedOrigin(publicUrl: string): string {
  try {
    const url = new URL(publicUrl.trim())
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.origin : ''
  } catch {
    return ''
  }
}

/** The base of every link to share: the server's public address, else the page's own origin. */
export function resolveShareOrigin(publicUrl: string | undefined, pageOrigin: string): string {
  return announcedOrigin(publicUrl ?? '') || pageOrigin
}
