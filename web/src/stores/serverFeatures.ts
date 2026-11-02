import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { decodeServerHealth } from '../types/rest'
import { syncDocumentIcons } from '../lib/communityIcon'

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
  const names = computed(() => new Set(list.value))
  let pending: Promise<void> | null = null

  function has(name: string): boolean {
    return names.value.has(name)
  }

  /** Adopts the icon version the server announced and moves the tab icons with it. */
  function setCommunityIcon(version: string) {
    communityIcon.value = version
    syncDocumentIcons(version)
  }

  /** Asks the server once; later calls share the answer. */
  function load(): Promise<void> {
    pending ??= api('/api/health', { decode: decodeServerHealth }).then(
      health => {
        list.value = health.features ?? []
        communityName.value = health.community_name?.trim() ?? ''
        setCommunityIcon(health.community_icon ?? '')
        loaded.value = true
      },
      () => {
        // The next call asks again; nothing is shown that was not confirmed.
        pending = null
      }
    )
    return pending
  }

  return {
    list, loaded, communityName, communityIcon, setCommunityIcon, has, load,
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
    notifyLevelsV2: computed(() => has('notify.levels.v2'))
  }
})
