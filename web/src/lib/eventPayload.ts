// What the event form sends: the fields as typed, checked, and (when editing) reduced to
// the ones that differ from the event, so an untouched start or place cannot re-anchor a series.
import type { CommunityEvent } from '../types/domain'
import { t } from '../i18n'
import { browserZone, untilIso } from './eventRecurrence'

export interface EventFields {
  title: string
  description: string
  startTime: string
  endTime: string
  locationType: 'talk' | 'text'
  channelId: string
  locationText: string
  repeat: string
  repeatUntil: string
}

/** The fields that make up the event itself (the cover and the roles are stored by their own calls). */
export const EVENT_FIELD_KEYS: (keyof EventFields)[] = ['title', 'description', 'startTime', 'endTime', 'locationType', 'channelId', 'locationText', 'repeat', 'repeatUntil']

export function toLocalIso(dateStr?: string | null): string {
  if (!dateStr) return ''
  const d = new Date(dateStr)
  if (isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** The fields of an event as the form shows them; blank for a new one. */
export function fieldsOf(event?: CommunityEvent | null): EventFields {
  return {
    title: event?.title ?? '',
    description: event?.description ?? '',
    startTime: toLocalIso(event?.start_time),
    endTime: toLocalIso(event?.end_time),
    locationType: event?.channel_id ? 'talk' : 'text',
    channelId: event?.channel_id ?? '',
    locationText: event?.location ?? '',
    repeat: '',
    repeatUntil: ''
  }
}

/** A sentence naming what is wrong, or null. */
export function validateFields(f: EventFields): string | null {
  if (!f.title.trim()) return t('events.errTitle')
  if (!f.startTime) return t('events.errStart')
  if (f.endTime && new Date(f.endTime).getTime() < new Date(f.startTime).getTime()) return t('events.errEnd')
  if (f.locationType === 'talk' && !f.channelId) return t('events.selectTalk')
  if (f.locationType === 'text' && !f.locationText.trim()) return t('events.errLocation')
  return null
}

function recurrence(f: EventFields) {
  const zone = browserZone()
  const until = untilIso(f.repeatUntil)
  return { frequency: f.repeat as 'weekly' | 'biweekly' | 'monthly', ...(zone ? { timezone: zone } : {}), ...(until ? { until } : {}) }
}

export function payloadOf(f: EventFields, withRecurrence: boolean) {
  return {
    title: f.title.trim(),
    description: f.description,
    start_time: new Date(f.startTime).toISOString(),
    end_time: f.endTime ? new Date(f.endTime).toISOString() : null,
    channel_id: f.locationType === 'talk' ? f.channelId : null,
    location: f.locationType === 'text' ? f.locationText.trim() : null,
    ...(withRecurrence && f.repeat ? { recurrence: recurrence(f) } : {})
  }
}

/** An edit names only what changed. */
export function changedFields(full: ReturnType<typeof payloadOf>, current: CommunityEvent) {
  const same = (a: string | null | undefined, b: string | null | undefined) => (a ? new Date(a).getTime() : null) === (b ? new Date(b).getTime() : null)
  return {
    ...(full.title !== current.title ? { title: full.title } : {}),
    ...(full.description !== current.description ? { description: full.description } : {}),
    ...(!same(full.start_time, current.start_time) ? { start_time: full.start_time } : {}),
    ...(!same(full.end_time, current.end_time) ? { end_time: full.end_time } : {}),
    ...((full.channel_id ?? null) !== (current.channel_id ?? null) || (full.location ?? null) !== (current.location ?? null) ? { channel_id: full.channel_id, location: full.location } : {})
  }
}
