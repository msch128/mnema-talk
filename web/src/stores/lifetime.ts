import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../shared/lib/api'
import { MAX_LIFETIME_SECONDS, validLifetime } from '../shared/lib/lifetime'
import { decodeChatLifetimeSettings } from '../types/rest'
import { useServerFeaturesStore } from './serverFeatures'

// Messages with a lifetime (doc/timed-messages.md): whether the community
// allows them and how long, and the lifetime this member chose for each
// conversation. The choice stays on this device (a convenience, not a
// setting): an unreadable store just starts with none.
const STORAGE_KEY = 'mnema.lifetimes.v1'
// The most choices kept: the least recently chosen go first (threads add up).
const MAX_CHOICES = 200

function readChoices(): Record<string, number> {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}')
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {}
    return Object.fromEntries(Object.entries(raw).filter(([, v]) => typeof v === 'number' && validLifetime(v)))
  } catch {
    return {}
  }
}

function writeChoices(choices: Record<string, number>) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(choices))
  } catch {
    // Storage may be full or blocked: the choice then lasts for this page.
  }
}

/** The key a thread's lifetime choice is kept under, apart from its channel's. */
export const threadLifetimeKey = (threadId: string) => `thread:${threadId}`

export const useLifetimeStore = defineStore('lifetime', () => {
  const features = useServerFeaturesStore()
  const enabled = ref(false)
  const maxSeconds = ref(MAX_LIFETIME_SECONDS)
  const choices = ref<Record<string, number>>(readChoices())
  let generation = 0
  let loaded: Promise<void> | null = null

  /** The server knows lifetimes and the community allows them. */
  const available = computed(() => features.has('messages.lifetime') && enabled.value)

  async function load() {
    const gen = ++generation
    // The list of features is loaded by the app; without it nothing is offered.
    if (!features.has('messages.lifetime')) {
      enabled.value = false
      return
    }
    try {
      const s = await api('/api/message-settings', { decode: decodeChatLifetimeSettings })
      if (gen !== generation) return
      enabled.value = s.message_lifetimes_enabled
      maxSeconds.value = s.message_lifetime_max_seconds
    } catch {
      // Without the settings the composer simply offers no lifetime.
      if (gen === generation) enabled.value = false
    }
  }

  /** Loads the settings once; channels_changed loads them again (load). */
  function ensure(): Promise<void> {
    loaded ??= load()
    return loaded
  }

  /** The lifetime chosen for a conversation or a thread (threadLifetimeKey), 0 for none; a choice the community no longer allows counts as none. */
  function forChannel(channelId: string | undefined | null): number {
    if (!channelId || !available.value) return 0
    const s = choices.value[channelId] ?? 0
    return validLifetime(s, maxSeconds.value) ? s : 0
  }

  /** Remembers a lifetime for a conversation; 0 takes it off. */
  function choose(channelId: string, seconds: number) {
    const next = { ...choices.value }
    delete next[channelId] // a new choice moves to the end: the order is the use
    if (seconds > 0 && validLifetime(seconds, maxSeconds.value)) next[channelId] = seconds
    for (const key of Object.keys(next).slice(0, Math.max(0, Object.keys(next).length - MAX_CHOICES))) delete next[key]
    choices.value = next
    writeChoices(next)
  }

  return { enabled, maxSeconds, available, load, ensure, forChannel, choose }
})
