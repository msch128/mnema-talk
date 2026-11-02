<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Calendar, Check, Edit3, HelpCircle, MapPin, Volume2, X } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import MarkdownContent from './MarkdownContent.vue'
import UserAvatar from './UserAvatar.vue'
import { useEventsStore } from '../stores/events'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { confirm } from '../lib/confirm'
import type { CommunityEvent, RSVPItem } from '../types/domain'
import { eventAvatar } from '../lib/eventAvatar'
import { formatEventStart, relativeEventDay } from '../lib/eventTime'
import { caughtErrorMessage } from '../lib/api'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { useAuthStore } from '../stores/auth'
import EventRoster from './EventRoster.vue'
import EventRolesDialog from './EventRolesDialog.vue'
import { locale, t } from '../i18n'

const props = defineProps<{
  event: CommunityEvent
}>()

const emit = defineEmits<{
  close: []
  edit: [event: CommunityEvent]
  join: [channelId: string]
}>()

const eventsStore = useEventsStore()
const chatStore = useChatStore()
const permissions = usePermissionsStore()
const toast = useToastStore()
const features = useServerFeaturesStore()
const auth = useAuthStore()
const editingRoles = ref(false)

// The creator and moderators change the roster.
const canManageRoster = computed(() => permissions.can('manage_events') || (!!auth.user && currentEvent.value.created_by === auth.user.id))
const showRoster = computed(() => features.eventRoles && ((currentEvent.value.roles ?? []).length > 0 || canManageRoster.value))

const currentEvent = computed(() => {
  return eventsStore.events.find(e => e.id === props.event.id) || props.event
})

const talkChannel = computed(() => {
  if (!currentEvent.value.channel_id) return null
  return chatStore.allChannels.find(c => c.id === currentEvent.value.channel_id)
})

const rsvps = ref<RSVPItem[]>([])
const loadingRsvps = ref(false)
const rsvpsFailed = ref(false)
let rsvpLoadSeq = 0

// Only the latest load may write: a slower, older answer would show stale data.
async function loadRsvps() {
  const seq = ++rsvpLoadSeq
  loadingRsvps.value = true
  try {
    const list = await eventsStore.listRSVPs(currentEvent.value.id)
    if (seq !== rsvpLoadSeq) return
    rsvps.value = list
    rsvpsFailed.value = false
  } catch (err) {
    if (seq !== rsvpLoadSeq) return
    console.warn('Failed to load RSVPs', err)
    rsvpsFailed.value = true
  } finally {
    if (seq === rsvpLoadSeq) loadingRsvps.value = false
  }
}

// The sign-ups come with the event's details only; the list and the live updates leave them out.
async function loadSignups() {
  if (!features.eventRoles) return
  try {
    await eventsStore.fetchDetail(currentEvent.value.id)
  } catch (err) {
    console.warn('Failed to load the sign-ups', err)
  }
}

onMounted(() => {
  void loadRsvps()
  void loadSignups()
})

// Someone signed up or left: read the sign-ups again.
watch(() => JSON.stringify((currentEvent.value.roles ?? []).map(r => [r.id, r.confirmed, r.waitlist])), () => { void loadSignups() })

function formatDateTime(iso: string): string {
  return formatEventStart(iso, locale.value)
}

const whenHint = computed(() => {
  const status = currentEvent.value.status
  return status === 'scheduled' || status === 'active' ? relativeEventDay(currentEvent.value.start_time, Date.now(), locale.value) : ''
})

// The RSVP buttons are the member's own answer; the counts sit in a line of their own.
const rsvpChoices = computed(() => [
  { id: 'going' as const, label: t('events.rsvpGoing'), icon: Check, on: 'border-emerald-500 bg-emerald-500/10 text-emerald-400' },
  { id: 'interested' as const, label: t('events.rsvpInterested'), icon: HelpCircle, on: 'border-amber-500 bg-amber-500/10 text-amber-400' },
  { id: 'not_going' as const, label: t('events.rsvpNotGoing'), icon: X, on: 'border-red-500 bg-red-500/10 text-red-400' }
])
const counts = computed(() => currentEvent.value.rsvp_counts)

const rsvpBusy = ref(false)

async function setRSVP(response: 'going' | 'interested' | 'not_going') {
  if (rsvpBusy.value) return
  rsvpBusy.value = true
  try {
    if (currentEvent.value.my_rsvp === response) {
      await eventsStore.deleteRSVP(currentEvent.value.id)
    } else {
      await eventsStore.setRSVP(currentEvent.value.id, response)
    }
    await loadRsvps()
  } catch (err) {
    toast.error(caughtErrorMessage(err, t('events.rsvpFailed')))
  } finally {
    rsvpBusy.value = false
  }
}

async function cancelEvent() {
  const confirmed = await confirm({
    title: t('events.cancel'),
    body: t('events.cancelConfirm'),
    confirmLabel: t('events.cancel'),
    cancelLabel: t('events.cancelKeep'),
    danger: true
  })
  if (!confirmed) return
  try {
    await eventsStore.cancel(currentEvent.value.id)
    toast.success(t('events.cancelled'))
  } catch (err) {
    toast.error(caughtErrorMessage(err, t('events.cancelFailed')))
  }
}
</script>

<template>
  <BaseDialog
    :title="currentEvent.title"
    panel-class="max-w-2xl"
    @close="emit('close')"
  >
    <div class="flex flex-col overflow-y-auto p-6 max-h-[80vh]">
      <!-- Cover Image -->
      <div v-if="currentEvent.cover_url" class="relative -mx-6 -mt-6 mb-6 aspect-[3/1] w-[calc(100%+3rem)] shrink-0 overflow-hidden border-b border-mnema-hairline bg-mnema-surface">
        <img :src="currentEvent.cover_url" :alt="t('events.coverAlt')" class="h-full w-full object-cover" />
      </div>

      <!-- Status & Time & Location Bar -->
      <div class="mb-4 flex flex-wrap items-center gap-3">
        <span
          v-if="currentEvent.status === 'active'"
          class="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-semibold text-emerald-400"
        >
          <span class="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse"></span>
          {{ $t('events.active') }}
        </span>
        <span
          v-else-if="currentEvent.status === 'scheduled'"
          class="inline-flex items-center rounded-full bg-mnema-accent/10 px-2.5 py-0.5 text-xs font-semibold text-mnema-accent"
        >
          {{ $t('events.scheduled') }}
        </span>
        <span
          v-else-if="currentEvent.status === 'cancelled'"
          class="inline-flex items-center rounded-full bg-red-500/10 px-2.5 py-0.5 text-xs font-semibold text-red-400"
        >
          {{ $t('events.cancelled') }}
        </span>
        <span
          v-else
          class="inline-flex items-center rounded-full bg-mnema-hairline px-2.5 py-0.5 text-xs font-semibold text-mnema-tertiary"
        >
          {{ $t('events.completed') }}
        </span>

        <!-- Time -->
        <div class="flex items-center gap-1.5 text-sm text-mnema-secondary">
          <Calendar class="w-4 h-4 text-mnema-tertiary" />
          <span>{{ formatDateTime(currentEvent.start_time) }}</span>
          <span v-if="whenHint" class="text-mnema-tertiary">· {{ whenHint }}</span>
          <span v-if="currentEvent.end_time">- {{ formatDateTime(currentEvent.end_time) }}</span>
        </div>

        <!-- Location -->
        <div v-if="talkChannel" class="flex items-center gap-1.5 text-sm text-mnema-accent font-medium">
          <Volume2 class="w-4 h-4" />
          <span>{{ talkChannel.name }}</span>
        </div>
        <div v-else-if="currentEvent.location" class="flex items-center gap-1.5 text-sm text-mnema-secondary">
          <MapPin class="w-4 h-4 text-mnema-tertiary" />
          <span>{{ currentEvent.location }}</span>
        </div>
      </div>

      <!-- Join Talk Button if active -->
      <div v-if="currentEvent.channel_id && currentEvent.status === 'active'" class="mb-6">
        <button
          type="button"
          class="flex w-full items-center justify-center gap-2 rounded-xl bg-emerald-600 px-4 py-2.5 text-sm font-semibold text-white shadow-lg shadow-emerald-900/20 hover:bg-emerald-500 transition-colors"
          @click="emit('join', currentEvent.channel_id!); emit('close')"
        >
          <Volume2 class="w-5 h-5" />
          <span>{{ $t('events.join') }} ({{ talkChannel?.name || $t('events.talkFallback') }})</span>
        </button>
      </div>

      <!-- Markdown Description -->
      <div class="mb-6 rounded-xl border border-mnema-border bg-mnema-surface p-4">
        <MarkdownContent
          v-if="currentEvent.description"
          :content="currentEvent.description"
          class="text-sm text-mnema-text"
        />
        <p v-else class="text-sm text-mnema-tertiary italic">
          {{ $t('events.noDescription') }}
        </p>
      </div>

      <EventRoster v-if="showRoster" :event="currentEvent" :can-manage="canManageRoster" @edit-roles="editingRoles = true" @gone="emit('close')" />

      <!-- RSVP Section -->
      <div class="mb-6 flex flex-col gap-3 rounded-xl border border-mnema-border bg-mnema-surface p-4">
        <div class="text-sm font-medium text-mnema-text">{{ $t('events.rsvp') }}</div>
        <div class="grid grid-cols-3 gap-2">
          <button
            v-for="c in rsvpChoices"
            :key="c.id"
            type="button"
            :data-rsvp="c.id"
            :disabled="rsvpBusy"
            :aria-pressed="currentEvent.my_rsvp === c.id ? 'true' : 'false'"
            :class="[
              'flex min-w-0 flex-col items-center justify-center gap-1 rounded-lg border px-1 py-2 text-center text-xs font-medium leading-tight transition-colors sm:flex-row sm:gap-1.5 sm:text-sm',
              currentEvent.my_rsvp === c.id ? `${c.on} font-semibold` : 'border-mnema-border hover:bg-mnema-hover text-mnema-secondary'
            ]"
            @click="setRSVP(c.id)"
          >
            <component :is="c.icon" class="h-4 w-4 shrink-0" />
            <span class="wrap-break-word">{{ c.label }}</span>
          </button>
        </div>
        <p class="text-xs text-mnema-tertiary" data-testid="event-rsvp-counts">
          {{ $t('events.goingCount', { count: counts.going }) }} · {{ $t('events.interestedCount', { count: counts.interested }) }}
        </p>
      </div>

      <!-- Participants List -->
      <div class="mb-4">
        <h3 class="mb-2 text-sm font-medium text-mnema-text">
          {{ $t('events.participants') }}
        </h3>
        <div v-if="loadingRsvps" class="py-4 text-center text-sm text-mnema-tertiary">
          {{ $t('common.loading') }}
        </div>
        <div v-else-if="rsvpsFailed" role="alert" class="text-sm text-red-400">
          {{ $t('events.participantsFailed') }}
        </div>
        <div v-else-if="rsvps.length === 0" class="text-sm text-mnema-tertiary italic">
          {{ $t('events.noResponses') }}
        </div>
        <div v-else class="flex flex-col divide-y divide-mnema-hairline rounded-lg border border-mnema-border bg-mnema-surface max-h-48 overflow-y-auto">
          <div
            v-for="item in rsvps"
            :key="item.user.id"
            class="flex items-center justify-between p-2.5 text-sm"
          >
            <div class="flex items-center gap-2 min-w-0">
              <UserAvatar :user="eventAvatar(item.user)" size="sm" />
              <span class="truncate font-medium text-mnema-text">
                {{ item.user.display_name || item.user.username }}
              </span>
            </div>
            <span
              :class="[
                'text-xs font-medium px-2 py-0.5 rounded-full',
                item.response === 'going' ? 'bg-emerald-500/10 text-emerald-400' :
                item.response === 'interested' ? 'bg-amber-500/10 text-amber-400' : 'bg-red-500/10 text-red-400'
              ]"
            >
              {{ $t(`events.${item.response === 'not_going' ? 'notGoing' : item.response}`) }}
            </span>
          </div>
        </div>
      </div>

      <!-- Moderator Actions -->
      <div v-if="permissions.can('manage_events')" class="mt-4 flex justify-between gap-3 border-t border-mnema-hairline pt-4">
        <button
          v-if="currentEvent.status !== 'cancelled' && currentEvent.status !== 'completed'"
          type="button"
          class="rounded-lg border border-red-500/30 px-3 py-1.5 text-sm font-medium text-red-400 hover:bg-red-500/10 transition-colors"
          @click="cancelEvent"
        >
          {{ $t('events.cancel') }}
        </button>
        <div class="flex-1"></div>
        <button
          type="button"
          class="flex items-center gap-1.5 rounded-lg border border-mnema-border bg-mnema-surface px-4 py-1.5 text-sm font-medium text-mnema-text hover:bg-mnema-hover transition-colors"
          @click="emit('edit', currentEvent)"
        >
          <Edit3 class="w-4 h-4 text-mnema-tertiary" />
          <span>{{ $t('events.edit') }}</span>
        </button>
      </div>
    </div>
    <EventRolesDialog v-if="editingRoles" :event="currentEvent" @close="editingRoles = false" />
  </BaseDialog>
</template>
