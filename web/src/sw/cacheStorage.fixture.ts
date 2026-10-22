// An in-memory CacheStorage for the service worker tests: keyed by URL, enough
// of the real interface for what the worker calls.

class MemoryCache {
  readonly entries = new Map<string, Response>()

  private key(request: RequestInfo | URL): string {
    const raw = typeof request === 'string' ? request : request instanceof URL ? request.href : request.url
    const url = new URL(raw, 'https://app.test')
    return url.pathname
  }

  async match(request: RequestInfo | URL): Promise<Response | undefined> {
    return this.entries.get(this.key(request))?.clone()
  }

  async put(request: RequestInfo | URL, response: Response): Promise<void> {
    this.entries.set(this.key(request), response)
  }

  async addAll(requests: readonly Request[]): Promise<void> {
    for (const request of requests) this.entries.set(this.key(request), new Response(`stored ${this.key(request)}`))
  }
}

export class MemoryCaches {
  readonly stores = new Map<string, MemoryCache>()

  async open(name: string): Promise<Cache> {
    let store = this.stores.get(name)
    if (!store) {
      store = new MemoryCache()
      this.stores.set(name, store)
    }
    return store as unknown as Cache
  }

  async keys(): Promise<string[]> {
    return [...this.stores.keys()]
  }

  async delete(name: string): Promise<boolean> {
    return this.stores.delete(name)
  }

  async match(request: RequestInfo | URL, options?: { cacheName?: string }): Promise<Response | undefined> {
    const names = options?.cacheName ? [options.cacheName] : [...this.stores.keys()]
    for (const name of names) {
      const hit = await this.stores.get(name)?.match(request)
      if (hit) return hit
    }
    return undefined
  }

  asStorage(): CacheStorage {
    return this as unknown as CacheStorage
  }
}
