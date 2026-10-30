<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Calendar, Check, HelpCircle, MapPin, Plus, Volume2 } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import EmptyState from './EmptyState.vue'
import EventDetailModal from './EventDetailModal.vue'
import EventEditDialog from './EventEditDialog.vue'
import UserAvatar from './UserAvatar.vue'
import { useEventsStore } from '../stores/events'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { useWebRTC } from '../composables/useWebRTC'
import { eventAvatar } from '../lib/eventAvatar'
import { formatEventStart, relativeEventDay } from '../lib/eventTime'
import { caughtErrorMessage } from '../lib/api'
import { locale, t } from '../i18n'
import type { CommunityEvent } from '../types/domain'

const emit = defineEmits<{
  close: []
}>()

const eventsStore = useEventsStore()
const chatStore = useChatStore()
const permissions = usePermissionsStore()
const toast = useToastStore()
const { joinVoiceChannel } = useWebRTC()

const activeTab = ref<'upcoming' | 'past'>('upcoming')
const selectedEvent = ref<CommunityEvent | null>(null)
const editingEvent = ref<CommunityEvent | null>(null)
const creatingEvent = ref(false)

onMounted(() => {
  void eventsStore.load()
})

watch(activeTab, tab => {
  if (tab === 'past' && !eventsStore.pastLoaded) void eventsStore.loadPast()
})

const displayedEvents = computed(() => {
  return activeTab.value === 'upcoming' ? eventsStore.upcomingEvents : eventsStore.pastEvents
})

// One dialog at a time: the list gives way to the detail or the form and
// comes back when that closes, so no title shows through from underneath.
const childOpen = computed(() => !!selectedEvent.value || !!editingEvent.value || creatingEvent.value)

function startText(iso: string): string {
  return formatEventStart(iso, locale.value)
}

function whenHint(event: CommunityEvent): string {
  return event.status === 'scheduled' || event.status === 'active' ? relativeEventDay(event.start_time, Date.now(), locale.value) : ''
}

function rsvpSummary(event: CommunityEvent): string {
  const parts = []
  if (event.rsvp_counts.going > 0) parts.push(t('events.goingCount', { count: event.rsvp_counts.going }))
  if (event.rsvp_counts.interested > 0) parts.push(t('events.interestedCount', { count: event.rsvp_counts.interested }))
  return parts.join(' · ')
}

function closeChild() {
  selectedEvent.value = null
  editingEvent.value = null
  creatingEvent.value = false
}

function editFromDetail(event: CommunityEvent) {
  selectedEvent.value = null
  editingEvent.value = event
}

function getChannelName(id?: string | null): string {
  if (!id) return ''
  const ch = chatStore.allChannels.find(c => c.id === id)
  return ch ? ch.name : t('events.talkFallback')
}

function handleJoin(channelId: string) {
  joinVoiceChannel(channelId)
  emit('close')
}

async function quickRSVP(event: CommunityEvent, response: 'going' | 'interested' | 'not_going') {
  try {
    if (event.my_rsvp === response) {
      await eventsStore.deleteRSVP(event.id)
    } else {
      await eventsStore.setRSVP(event.id, response)
    }
  } catch (err) {
    toast.error(caughtErrorMessage(err, t('events.rsvpFailed')))
  }
}
</script>

<template>
  <BaseDialog
    v-if="!childOpen"
    :title="$t('events.events')"
    panel-class="max-w-2xl max-h-[85vh]"
    @close="emit('close')"
  >
    <template #badge>
      <button
        v-if="permissions.can('manage_events')"
        type="button"
        class="flex items-center justify-center gap-1 rounded-md bg-mnema-accent px-2.5 py-1 text-xs font-semibold text-white hover:opacity-90 transition-opacity max-md:h-9 max-md:w-9 max-md:p-0"
        :aria-label="$t('events.create')"
        @click="creatingEvent = true"
      >
        <Plus class="w-3.5 h-3.5 max-md:h-4 max-md:w-4" />
        <span class="max-md:hidden">{{ $t('events.create') }}</span>
      </button>
    </template>

    <div class="flex flex-col min-h-0 flex-1 overflow-hidden">
      <!-- Tabs -->
      <div class="flex shrink-0 border-b border-mnema-hairline px-4 pt-3 sm:px-6">
        <button
          type="button"
          :class="[
            'border-b-2 px-4 py-2 text-sm font-medium transition-colors cursor-pointer',
            activeTab === 'upcoming'
              ? 'border-mnema-accent text-mnema-text font-semibold'
              : 'border-transparent text-mnema-tertiary hover:text-mnema-text'
          ]"
          @click="activeTab = 'upcoming'"
        >
          {{ $t('events.upcoming') }} ({{ eventsStore.upcomingCount }})
        </button>
        <button
          type="button"
          :class="[
            'border-b-2 px-4 py-2 text-sm font-medium transition-colors cursor-pointer',
            activeTab === 'past'
              ? 'border-mnema-accent text-mnema-text font-semibold'
              : 'border-transparent text-mnema-tertiary hover:text-mnema-text'
          ]"
          @click="activeTab = 'past'"
        >
          {{ $t('events.past') }}
        </button>
      </div>

      <!-- Events List -->
      <div class="flex-1 overflow-y-auto p-4 sm:p-6 flex flex-col gap-4">
        <div v-if="(activeTab === 'past' ? eventsStore.pastLoading && !eventsStore.pastLoaded : eventsStore.loading) && displayedEvents.length === 0" class="py-8 text-center text-sm text-mnema-tertiary" role="status">
          {{ $t('common.loading') }}
        </div>

        <EmptyState
          v-else-if="displayedEvents.length === 0"
          :icon="Calendar"
          :text="activeTab === 'upcoming' ? $t('events.noUpcoming') : $t('events.noPast')"
          :hint="activeTab === 'upcoming' ? $t('events.whatAreEvents') : ''"
        >
          <button
            v-if="activeTab === 'upcoming' && permissions.can('manage_events')"
            type="button"
            class="btn btn-primary btn-sm flex items-center gap-1"
            data-testid="events-empty-create"
            @click="creatingEvent = true"
          >
            <Plus class="w-3.5 h-3.5" />
            <span>{{ $t('events.create') }}</span>
          </button>
        </EmptyState>

        <article
          v-for="event in displayedEvents"
          :key="event.id"
          class="flex shrink-0 flex-col overflow-hidden rounded-xl border border-mnema-border bg-mnema-surface shadow-xs hover:border-mnema-hairline transition-colors"
        >
          <!-- Cover -->
          <div
            v-if="event.cover_url"
            class="aspect-[3/1] w-full shrink-0 overflow-hidden bg-mnema-raised cursor-pointer"
            @click="selectedEvent = event"
          >
            <img :src="event.cover_url" :alt="event.title" class="h-full w-full object-cover" />
          </div>

          <div class="flex flex-col gap-3 p-4">
            <!-- Header: Title & Badges -->
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <h3 class="truncate text-base font-semibold text-mnema-text">
                  <button
                    type="button"
                    class="max-w-full truncate hover:text-mnema-accent cursor-pointer"
                    @click="selectedEvent = event"
                  >
                    {{ event.title }}
                  </button>
                </h3>
                <div class="mt-1 flex flex-wrap items-center gap-3 text-xs text-mnema-secondary">
                  <div class="flex items-center gap-1">
                    <Calendar class="w-3.5 h-3.5 text-mnema-tertiary" />
                    <span>{{ startText(event.start_time) }}</span>
                    <span v-if="whenHint(event)" class="text-mnema-tertiary">· {{ whenHint(event) }}</span>
                  </div>
                  <div v-if="event.channel_id" class="flex items-center gap-1 font-medium text-mnema-accent">
                    <Volume2 class="w-3.5 h-3.5" />
                    <span>{{ getChannelName(event.channel_id) }}</span>
                  </div>
                  <div v-else-if="event.location" class="flex items-center gap-1">
                    <MapPin class="w-3.5 h-3.5 text-mnema-tertiary" />
                    <span class="truncate max-w-40">{{ event.location }}</span>
                  </div>
                </div>
              </div>

              <!-- Status Badge -->
              <span
                v-if="event.status === 'active'"
                class="shrink-0 inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-semibold text-emerald-400"
              >
                <span class="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse"></span>
                {{ $t('events.active') }}
              </span>
              <span
                v-else-if="event.status === 'cancelled'"
                class="shrink-0 rounded-full bg-red-500/10 px-2 py-0.5 text-xs font-semibold text-red-400"
              >
                {{ $t('events.cancelled') }}
              </span>
              <span
                v-else-if="event.status === 'completed'"
                class="shrink-0 rounded-full bg-mnema-hairline px-2 py-0.5 text-xs font-semibold text-mnema-tertiary"
              >
                {{ $t('events.completed') }}
              </span>
            </div>

            <!-- Description Preview -->
            <p
              v-if="event.description"
              class="line-clamp-2 text-xs text-mnema-secondary cursor-pointer"
              @click="selectedEvent = event"
            >
              {{ event.description }}
            </p>

            <!-- Who answered: avatars of the members who are going, and the counts -->
            <div v-if="event.rsvp_counts.going > 0 || event.rsvp_counts.interested > 0" class="flex items-center gap-2" data-testid="event-rsvp-summary">
              <div v-if="event.going_members?.length" class="flex -space-x-1.5 overflow-hidden">
                <UserAvatar
                  v-for="m in event.going_members.slice(0, 5)"
                  :key="m.id"
                  :user="eventAvatar(m)"
                  size="xs"
                />
              </div>
              <span class="text-xs text-mnema-tertiary">
                {{ rsvpSummary(event) }}
              </span>
            </div>

            <!-- Card Bottom Bar: Join / RSVP / Details -->
            <div class="mt-1 flex flex-wrap items-center justify-between gap-2 border-t border-mnema-hairline pt-3">
              <div class="flex items-center gap-1.5">
                <button
                  type="button"
                  :class="[
                    'flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
                    event.my_rsvp === 'going'
                      ? 'bg-emerald-500/20 text-emerald-400 font-semibold'
                      : 'bg-mnema-raised text-mnema-secondary hover:bg-mnema-hover'
                  ]"
                  :aria-pressed="event.my_rsvp === 'going' ? 'true' : 'false'"
                  @click="quickRSVP(event, 'going')"
                >
                  <Check class="w-3.5 h-3.5" />
                  <span>{{ $t('events.rsvpGoing') }}</span>
                </button>
                <button
                  type="button"
                  :class="[
                    'flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium transition-colors',
                    event.my_rsvp === 'interested'
                      ? 'bg-amber-500/20 text-amber-400 font-semibold'
                      : 'bg-mnema-raised text-mnema-secondary hover:bg-mnema-hover'
                  ]"
                  :aria-pressed="event.my_rsvp === 'interested' ? 'true' : 'false'"
                  @click="quickRSVP(event, 'interested')"
                >
                  <HelpCircle class="w-3.5 h-3.5" />
                  <span>{{ $t('events.rsvpInterested') }}</span>
                </button>
              </div>

              <div class="flex items-center gap-2">
                <!-- Join Button if Active Talk -->
                <button
                  v-if="event.channel_id && event.status === 'active'"
                  type="button"
                  class="flex items-center gap-1.5 rounded-lg bg-emerald-600 px-3 py-1 text-xs font-semibold text-white hover:bg-emerald-500 transition-colors"
                  @click="handleJoin(event.channel_id!)"
                >
                  <Volume2 class="w-3.5 h-3.5" />
                  <span>{{ $t('events.join') }}</span>
                </button>

                <button
                  type="button"
                  class="rounded-lg border border-mnema-border px-3 py-1 text-xs font-medium text-mnema-text hover:bg-mnema-hover transition-colors"
                  @click="selectedEvent = event"
                >
                  {{ $t('events.details') }}
                </button>
              </div>
            </div>
          </div>
        </article>

        <button
          v-if="activeTab === 'past' && eventsStore.pastHasMore && displayedEvents.length > 0"
          type="button"
          data-testid="events-load-more"
          :disabled="eventsStore.pastLoading"
          class="shrink-0 self-center rounded-lg px-4 py-2 text-sm font-medium text-mnema-tertiary transition-colors hover:text-mnema-text disabled:opacity-60 cursor-pointer"
          @click="eventsStore.loadPast(true)"
        >
          {{ $t('events.loadMorePast') }}
        </button>
      </div>
    </div>
  </BaseDialog>

  <EventDetailModal
    v-else-if="selectedEvent"
    :event="selectedEvent"
    @close="closeChild"
    @join="handleJoin"
    @edit="editFromDetail"
  />

  <EventEditDialog
    v-else
    :event="editingEvent"
    @close="closeChild"
  />
</template>