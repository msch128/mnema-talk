import { api } from './api'

// url -> Promise<preview|null>. Messages re-render and remount constantly
// (scrolling, jumping); each URL must hit the rate-limited endpoint only once.
const cache = new Map()
const MAX_ENTRIES = 300

export function fetchLinkPreview(url) {
  if (cache.has(url)) return cache.get(url)
  const p = api(`/api/link-preview?url=${encodeURIComponent(url)}`)
    .then(data => (data && (data.title || data.description) ? data : null))
    .catch(err => {
      // Transient failures (rate limit, network) may be retried later;
      // a definitive "no preview" is remembered.
      const status = err?.status
      if (!status || status === 429 || status >= 500) cache.delete(url)
      return null
    })
  if (cache.size >= MAX_ENTRIES) cache.delete(cache.keys().next().value)
  cache.set(url, p)
  return p
}

export function clearLinkPreviewCache() {
  cache.clear()
}
