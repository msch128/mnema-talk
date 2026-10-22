import { answerAsset, answerPage, dropOldShells, precache, shellCacheName, strategyFor } from './shell.ts'
import { onNotificationClick, onPush, onSubscriptionChange } from './push.ts'
import type { ClickEventLike, PushEventLike, PushScope, SubscriptionChangeEventLike } from './push.ts'

/** What the worker needs from its global scope; a test passes a stand-in. */
export interface WorkerScope extends PushScope {
  location: { origin: string }
  caches: CacheStorage
  clients: PushScope['clients'] & { claim(): Promise<void> }
  skipWaiting(): Promise<void>
  addEventListener(type: 'install' | 'activate', listener: (event: WaitEvent) => void): void
  addEventListener(type: 'fetch', listener: (event: FetchLike) => void): void
  addEventListener(type: 'push', listener: (event: PushEventLike) => void): void
  addEventListener(type: 'notificationclick', listener: (event: ClickEventLike) => void): void
  addEventListener(type: 'pushsubscriptionchange', listener: (event: SubscriptionChangeEventLike) => void): void
}

export interface WaitEvent {
  waitUntil(work: Promise<unknown>): void
}

export interface FetchLike {
  request: Request
  respondWith(response: Promise<Response>): void
}

/** What a build bakes into the worker. */
export interface WorkerConfig {
  /** Names this build's cache; a new build drops the old ones. */
  version: string
  /** The app shell's fixed files (the hashed assets index.html loads, icons, manifest). */
  precache: readonly string[]
}

function installShell(scope: WorkerScope, config: WorkerConfig, cacheName: string) {
  scope.addEventListener('install', event => {
    event.waitUntil(precache(scope.caches, cacheName, config.precache).then(() => scope.skipWaiting()))
  })
  scope.addEventListener('activate', event => {
    event.waitUntil(dropOldShells(scope.caches, cacheName).then(() => scope.clients.claim()))
  })
  const fixed = new Set(config.precache)
  scope.addEventListener('fetch', event => {
    const { request } = event
    const how = strategyFor({ method: request.method, url: new URL(request.url), mode: request.mode, origin: scope.location.origin, precached: fixed })
    if (how === 'network') return
    const fetchIt = () => scope.fetch(request)
    event.respondWith(how === 'shell'
      ? answerPage(scope.caches, cacheName, fetchIt)
      : answerAsset(scope.caches, cacheName, request, fetchIt))
  })
}

function installPush(scope: WorkerScope) {
  scope.addEventListener('push', event => event.waitUntil(onPush(scope, event)))
  scope.addEventListener('notificationclick', event => event.waitUntil(onNotificationClick(scope, event)))
  scope.addEventListener('pushsubscriptionchange', event => event.waitUntil(onSubscriptionChange(scope, event)))
}

/** Registers every handler of the service worker on its scope. */
export function installWorker(scope: WorkerScope, config: WorkerConfig) {
  installShell(scope, config, shellCacheName(config.version))
  installPush(scope)
}
