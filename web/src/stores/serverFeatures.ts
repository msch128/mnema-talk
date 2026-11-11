import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { decodeServerHealth, isServerHealthUnavailable, type ServerHealth, type ServerHealthUnavailable } from '../types/rest'
import { syncDocumentIcons } from '../lib/communityIcon'

/** How often the server is asked again while it reports a dependency as down. */
export const OUTAGE_RECHECK_MS = 30000

// The server describes itself in a 503 as well (a dependency is down). An older server sends only the error then, which is no answer.
function decodeHealth(value: unknown): ServerHealth | ServerHealthUnavailable {
  return isServerHealthUnavailable(value) ? value : decodeServerHealth(value)
}

/** One flag per optional feature the web app asks about. */
function featureFlags(has: (name: string) => boolean) {
  return {
    dm: computed(() => has('dm')),
    dmV2: computed(() => has('dm.v2')),
    blocks: computed(() => has('blocks')),
    reports: computed(() => has('reports')),
    automod: computed(() => has('automod')),
    moderationHistory: computed(() => has('moderation.history')),
    profileBanner: computed(() => has('profile.banner')),
    anonymousPolls: computed(() => has('polls.anonymous')),
    channelAutoDelete: computed(() => has('channels.auto_delete')),
    avatarRemove: computed(() => has('profile.avatar_remove')),
    talksLobby: computed(() => has('talks.lobby')),
    opsStatus: computed(() => has('ops.status')),
    importDiscord: computed(() => has('import.discord')),
    talksStage: computed(() => has('talks.stage')),
    talksReactions: computed(() => has('talks.reactions')),
    forum: computed(() => has('channels.forum')),
    channelsForum: computed(() => has('channels.forum')),
    events: computed(() => has('events')),
    eventRoles: computed(() => has('events.roles')),
    eventRecurrence: computed(() => has('events.recurrence')),
    wiki: computed(() => has('wiki')),
    notifyLevelsV2: computed(() => has('notify.levels.v2')),
    messageNonce: computed(() => has('messages.nonce')),
    reportsOpenCount: computed(() => has('reports.open_count'))
  }
}

// The optional features of the server this app talks to (GET /api/health
// `features`). A 1.0.x server sends no list: every newer feature is then
// absent and its UI stays hidden. Names never change meaning
// (doc/api-stability.md).
export const useServerFeaturesStore = defineStore('serverFeatures', () => {
  const list = ref<string[]>([])
  // False until the first answer: a feature is only "missing" once the list is known.
  const loaded = ref(false)
  // What the server calls itself; empty until the answer (or on an older server).
  const communityName = ref('')
  // The version of the community icon (GET /api/community/icon); empty while the Owner set none.
  const communityIcon = ref('')
  // The dependencies the server reports as down (`database`, `storage`); empty while it is healthy.
  const unavailable = ref<string[]>([])
  // Without object storage uploads, attachments and pictures fail; chat and Talks do not need it.
  const storageDown = computed(() => unavailable.value.includes('storage'))
  const names = computed(() => new Set(list.value))
  let pending: Promise<void> | null = null
  let recheck: ReturnType<typeof setTimeout> | undefined

  function has(name: string): boolean {
    return names.value.has(name)
  }

  /** Adopts the icon version the server announced and moves the tab icons with it. */
  function setCommunityIcon(version: string) {
    communityIcon.value = version
    syncDocumentIcons(version)
  }

  /** While a dependency is down the server is asked again, so the notice goes when the outage does. */
  function watchOutage() {
    clearTimeout(recheck)
    if (unavailable.value.length === 0) return
    recheck = setTimeout(() => {
      pending = null
      void load()
    }, OUTAGE_RECHECK_MS)
  }

  /** Asks the server once; later calls share the answer. */
  function load(): Promise<void> {
    pending ??= api('/api/health', { acceptStatus: [503], decode: decodeHealth }).then(
      health => {
        list.value = health.features ?? []
        communityName.value = health.community_name?.trim() ?? ''
        setCommunityIcon(health.community_icon ?? '')
        unavailable.value = health.status === 'unavailable' ? health.unavailable : []
        loaded.value = true
      },
      () => {
        // The next call asks again; nothing is shown that was not confirmed.
        pending = null
      }
    ).then(watchOutage)
    return pending
  }

  /**
   * Waits for the feature list when it has not been read yet (the first read may have failed, as during a server restart), but only for
   * `ms`: a send must not stall on a health check. Returns whether the list is known afterwards.
   */
  async function ensureLoaded(ms = 3000): Promise<boolean> {
    if (loaded.value) return true
    let timer: ReturnType<typeof setTimeout> | undefined
    await Promise.race([load(), new Promise<void>(resolve => { timer = setTimeout(resolve, ms) })])
    clearTimeout(timer)
    return loaded.value
  }

  return {
    list, loaded, unavailable, storageDown, ensureLoaded, communityName, communityIcon, setCommunityIcon, has, load,
    ...featureFlags(has)
  }
})
