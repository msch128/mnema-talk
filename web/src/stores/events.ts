import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import {
  decodeCommunityeventsCommunityEvent,
  isCommunityeventsCommunityEvent,
  isCommunityeventsRSVPItem,
  type CommunityeventsCommunityEvent as CommunityEvent,
  type CommunityeventsCreateEventRequest as CreateEventRequest,
  type CommunityeventsRSVPItem as RSVPItem,
  type CommunityeventsUpdateEventRequest as UpdateEventRequest
} from '../types/rest'
import type { ServerEventPayloads } from '../types/events'
import { useServerFeaturesStore } from './serverFeatures'
import { usePastEvents } from './eventsPast'
import { useToastStore } from './toast'
import { t } from '../i18n'
import { openEvents } from '../lib/eventsDialog'
import { eventRosterActions } from './eventsRoster'

/** Whole minutes until an event starts, at least one (the server reminds a quarter of an hour ahead). */
export function minutesUntil(startTime: string, now = Date.now()): number {
  const ms = new Date(startTime).getTime() - now
  return Number.isFinite(ms) ? Math.max(1, Math.round(ms / 60000)) : 15
}

const decodeEvents = arrayDecoder('community events', isCommunityeventsCommunityEvent)
const decodeRSVPs = arrayDecoder('event rsvps', isCommunityeventsRSVPItem)

async function apiFetchEvents(): Promise<CommunityEvent[]> {
  return api('/api/events', { decode: decodeEvents })
}

async function apiCreate(data: CreateEventRequest): Promise<CommunityEvent> {
  return api('/api/events', { method: 'POST', json: data, decode: decodeCommunityeventsCommunityEvent })
}

async function apiUpdate(id: string, data: UpdateEventRequest): Promise<CommunityEvent> {
  return api(`/api/events/${id}`, { method: 'PATCH', json: data, decode: decodeCommunityeventsCommunityEvent })
}

async function apiCancel(id: string): Promise<CommunityEvent> {
  return api(`/api/events/${id}/cancel`, { method: 'POST', decode: decodeCommunityeventsCommunityEvent })
}

async function apiSetRSVP(id: string, response: 'going' | 'interested' | 'not_going'): Promise<CommunityEvent> {
  return api(`/api/events/${id}/rsvp`, { method: 'PUT', json: { response }, decode: decodeCommunityeventsCommunityEvent })
}

async function apiDeleteRSVP(id: string): Promise<CommunityEvent> {
  return api(`/api/events/${id}/rsvp`, { method: 'DELETE', decode: decodeCommunityeventsCommunityEvent })
}

async function apiUploadCover(id: string, file: File): Promise<CommunityEvent> {
  const form = new FormData()
  form.append('cover', file)
  return api(`/api/events/${id}/cover`, { method: 'POST', form, decode: decodeCommunityeventsCommunityEvent })
}

async function apiDeleteCover(id: string): Promise<CommunityEvent> {
  return api(`/api/events/${id}/cover`, { method: 'DELETE', decode: decodeCommunityeventsCommunityEvent })
}

async function apiListRSVPs(id: string): Promise<RSVPItem[]> {
  return api(`/api/events/${id}/rsvps`, { decode: decodeRSVPs })
}

/** A pushed event carries no one's own answers or the sign-ups: keep the ones this client knows. */
function keepOwn(pushed: CommunityEvent, known: CommunityEvent | undefined): CommunityEvent {
  return {
    ...pushed,
    ...(known?.my_rsvp != null ? { my_rsvp: known.my_rsvp } : {}),
    ...(known?.my_signup ? { my_signup: known.my_signup } : {}),
    ...(known?.signups ? { signups: known.signups } : {})
  }
}

function isOngoing(e: CommunityEvent) {
  return e.status === 'scheduled' || e.status === 'active'
}

export const useEventsStore = defineStore('communityEvents', () => {
  const features = useServerFeaturesStore()
  const events = ref<CommunityEvent[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  const enabled = computed(() => features.events)
  const upcomingCount = computed(() => (enabled.value ? events.value.filter(isOngoing).length : 0))
  const upcomingEvents = computed(() => events.value.filter(isOngoing)
    .sort((a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime()))
  const past = usePastEvents(enabled, error)
  const pastEvents = computed(() => [...events.value.filter(e => !isOngoing(e)), ...past.fetched.value.filter(o => !events.value.some(e => e.id === o.id))]
    .sort((a, b) => new Date(b.start_time).getTime() - new Date(a.start_time).getTime()))

  function replaceOrAppend(item: CommunityEvent) {
    const idx = events.value.findIndex(e => e.id === item.id)
    if (idx >= 0) events.value.splice(idx, 1, item)
    else events.value.push(item)
    past.replace(item)
  }

  // Bumped by every load() and by reset(): only the latest answer is applied.
  let loadSeq = 0

  async function load() {
    if (!enabled.value) return
    const mine = ++loadSeq
    loading.value = true
    error.value = null
    try {
      const list = await apiFetchEvents()
      if (mine === loadSeq) events.value = list
    } catch (err) {
      if (mine === loadSeq) error.value = err instanceof Error ? err.message : t('events.loadFailed')
    } finally {
      if (mine === loadSeq) loading.value = false
    }
  }

  const act = async <T extends CommunityEvent>(fn: () => Promise<T>): Promise<T> => {
    const item = await fn()
    replaceOrAppend(item)
    return item
  }

  const roster = eventRosterActions(act)
  return {
    events, loading, error, enabled, upcomingCount, upcomingEvents, pastEvents, pastLoading: past.loading, pastLoaded: past.loaded, pastHasMore: past.hasMore, load, loadPast: past.load,
    create: (data: CreateEventRequest) => act(() => apiCreate(data)),
    update: (id: string, data: UpdateEventRequest) => act(() => apiUpdate(id, data)),
    cancel: (id: string) => act(() => apiCancel(id)),
    setRSVP: (id: string, resp: 'going' | 'interested' | 'not_going') => act(() => apiSetRSVP(id, resp)),
    deleteRSVP: (id: string) => act(() => apiDeleteRSVP(id)),
    uploadCover: (id: string, file: File) => act(() => apiUploadCover(id, file)),
    deleteCover: (id: string) => act(() => apiDeleteCover(id)),
    listRSVPs: (id: string) => apiListRSVPs(id),
    ...roster,
    // A pushed event carries no one's own answers: keep the ones this client knows.
    handleEventUpdate: (payload: CommunityEvent) => {
      replaceOrAppend(keepOwn(payload, events.value.find(e => e.id === payload.id)))
    },
    handleEventDelete: (id: string) => {
      events.value = events.value.filter(e => e.id !== id); past.remove(id)
    },
    handleEventReminder: (p: ServerEventPayloads['event_reminder']) => {
      useToastStore().info(
        t('events.reminderToast', { title: p.title, count: minutesUntil(p.start_time) }),
        { action: { label: t('events.reminderOpen'), onClick: () => openEvents(p.event_id) } }
      )
    },
    reset: () => {
      ++loadSeq
      events.value = []
      past.reset()
      loading.value = false
      error.value = null
    }
  }
})
