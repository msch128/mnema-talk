// The offline shell of the service worker: which requests it answers from its
// cache and how. Nothing from /api/ is ever cached (it carries personal data),
// and only same-origin GET requests are touched at all.

export const SHELL_PREFIX = 'mnema-shell-'
/** Cache key of the app shell (index.html), whatever route asked for it. */
export const SHELL_KEY = '/'

export type Strategy = 'shell' | 'asset' | 'network'

interface RequestFacts {
  method: string
  url: URL
  mode: string
  /** The worker's own origin. */
  origin: string
  /** Fixed files the worker cached when it installed. */
  precached: ReadonlySet<string>
}

/** How the worker answers a request: the shell for page loads, the cache for built assets, otherwise the network alone. */
export function strategyFor({ method, url, mode, origin, precached }: RequestFacts): Strategy {
  if (method !== 'GET' || url.origin !== origin) return 'network'
  if (url.pathname.startsWith('/api/')) return 'network'
  if (mode === 'navigate') return 'shell'
  if (url.pathname.startsWith('/assets/') || precached.has(url.pathname)) return 'asset'
  return 'network'
}

/** Name of the cache a worker version owns. */
export function shellCacheName(version: string): string {
  return `${SHELL_PREFIX}${version}`
}

/** Stores the fixed files of this build; the page itself is fetched fresh. */
export async function precache(caches: CacheStorage, name: string, urls: readonly string[]): Promise<void> {
  const cache = await caches.open(name)
  await cache.addAll(urls.map(url => new Request(url, { cache: 'reload' })))
}

/** Removes the caches of older worker versions. */
export async function dropOldShells(caches: CacheStorage, keep: string): Promise<void> {
  const names = await caches.keys()
  await Promise.all(names.filter(n => n.startsWith(SHELL_PREFIX) && n !== keep).map(n => caches.delete(n)))
}

function isShellPage(res: Response): boolean {
  return res.status === 200 && !res.redirected && (res.headers.get('content-type') ?? '').includes('text/html')
}

/** Page loads: the network first, so a deploy arrives at once; the cached shell when offline. */
export async function answerPage(caches: CacheStorage, name: string, fetchPage: () => Promise<Response>): Promise<Response> {
  try {
    const res = await fetchPage()
    if (isShellPage(res)) {
      const cache = await caches.open(name)
      await cache.put(SHELL_KEY, res.clone())
    }
    return res
  } catch {
    const cached = await caches.match(SHELL_KEY, { cacheName: name })
    return cached ?? Response.error()
  }
}

/** Built assets carry a hash in their name, so a cached copy never goes stale. */
export async function answerAsset(caches: CacheStorage, name: string, request: Request, fetchAsset: () => Promise<Response>): Promise<Response> {
  const cache = await caches.open(name)
  const hit = await cache.match(request)
  if (hit) return hit
  const res = await fetchAsset()
  if (res.status === 200) await cache.put(request, res.clone())
  return res
}
