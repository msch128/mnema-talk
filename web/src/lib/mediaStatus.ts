// Why a picture did not load. An <img> that fails says nothing about the
// reason, so the failing address is asked once more, for a single byte: 404
// and 410 (the media endpoint answers 410 for a file that was deleted or is
// missing from storage, 404 for one that never existed or may not be seen)
// mean the picture is gone for good; anything else (no network, 5xx, 429, an
// expired session) may pass, and the member can try again.

export type MediaFailure = 'gone' | 'failed'

// What is gone stays gone: a list of avatars shows one member many times and
// must not ask for each. Bounded so a long session does not grow it.
const gone = new Set<string>()
const GONE_MAX = 500
const asking = new Map<string, Promise<MediaFailure>>()

/** Whether this address is already known to be gone, so an element can skip the request. */
export function knownGone(url: string): boolean {
  return gone.has(url)
}

async function ask(url: string): Promise<MediaFailure> {
  try {
    const response = await fetch(url, { headers: { Range: 'bytes=0-0' }, credentials: 'same-origin', cache: 'no-store' })
    // Only the answer is wanted; a server that ignores Range would stream the picture.
    void response.body?.cancel().catch(() => undefined)
    // The link preview proxy says 404 for "no usable image" (hotlink block,
    // not a picture, too large): only its 410 means taken down.
    const goneStatus = url.startsWith('/api/link-preview/') ? response.status === 410 : response.status === 404 || response.status === 410
    if (!goneStatus) return 'failed'
    if (gone.size >= GONE_MAX) gone.clear()
    gone.add(url)
    return 'gone'
  } catch {
    return 'failed'
  }
}

/** Classifies a picture that failed to load. Concurrent calls for one address share a request. */
export function classifyMediaFailure(url: string): Promise<MediaFailure> {
  if (gone.has(url)) return Promise.resolve('gone')
  let pending = asking.get(url)
  if (!pending) {
    pending = ask(url).finally(() => asking.delete(url))
    asking.set(url, pending)
  }
  return pending
}

/** Forgets what was learned (tests). */
export function resetMediaStatus(): void {
  gone.clear()
  asking.clear()
}
