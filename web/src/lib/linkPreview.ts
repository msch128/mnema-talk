import { api } from './api'
import type { LinkPreview } from '../types/domain'
import { decodeLinkPreview } from '../types/domain'
import { isRecord, nullableDecoder } from '../types/validation'

// url -> Promise<preview|null>. Messages re-render and remount constantly
// (scrolling, jumping); each URL must hit the rate-limited endpoint only once.
const cache = new Map<string, Promise<LinkPreview | null>>()
const MAX_ENTRIES = 300

export function fetchLinkPreview(url: string): Promise<LinkPreview | null> {
  const cached = cache.get(url)
  if (cached) return cached
  const p = api(`/api/link-preview?url=${encodeURIComponent(url)}`, { decode: nullableDecoder(decodeLinkPreview) })
    .then(data => (data && (data.title || data.description) ? data : null))
    .catch(err => {
      // Transient failures (rate limit, network) may be retried later;
      // a definitive "no preview" is remembered.
      const status = isRecord(err) && typeof err.status === 'number' ? err.status : 0
      if (!status || status === 429 || status >= 500) cache.delete(url)
      return null
    })
  const oldest = cache.keys().next().value
  if (cache.size >= MAX_ENTRIES && oldest !== undefined) cache.delete(oldest)
  cache.set(url, p)
  return p
}

export function clearLinkPreviewCache() {
  cache.clear()
}
