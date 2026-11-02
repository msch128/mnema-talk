<script setup lang="ts">
import { computed, ref } from 'vue'
import { Pencil, X } from '@lucide/vue'
import type { CommunityEvent } from '../types/domain'
import type { CommunityeventsEventRole as EventRole, CommunityeventsSignupItem as SignupItem } from '../types/rest'
import { useEventsStore } from '../stores/events'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { eventAvatar } from '../lib/eventAvatar'
import { caughtErrorMessage } from '../lib/api'
import { t } from '../i18n'
import UserAvatar from './UserAvatar.vue'

// The roles of an event and who holds or awaits each: a card per role with its
// fill, a button to join, switch or leave, and the members. The creator and
// moderators remove members and open the role editor.
const props = defineProps<{
  event: CommunityEvent
  /** The member may change the roster (creator or Manage events). */
  canManage: boolean
}>()

const emit = defineEmits<{ 'edit-roles': [] }>()

const eventsStore = useEventsStore()
const auth = useAuthStore()
const toast = useToastStore()
const busy = ref(false)

const roles = computed(() => props.event.roles ?? [])
const open = computed(() => props.event.status === 'scheduled' || props.event.status === 'active')
const mine = computed(() => props.event.my_signup ?? null)
const signups = computed(() => props.event.signups ?? [])

function members(role: EventRole, state: 'confirmed' | 'waitlist'): SignupItem[] {
  return signups.value.filter(s => s.role_id === role.id && s.state === state)
}

function full(role: EventRole): boolean {
  return role.capacity != null && role.confirmed >= role.capacity
}

function fill(role: EventRole): string {
  return role.capacity != null ? `${role.confirmed}/${role.capacity}` : String(role.confirmed)
}

// Literal class names, so the stylesheet generator finds them (inline styles are not allowed by the CSP).
const FILL = ['w-0', 'w-[10%]', 'w-[20%]', 'w-[30%]', 'w-[40%]', 'w-[50%]', 'w-[60%]', 'w-[70%]', 'w-[80%]', 'w-[90%]', 'w-full']

function fillWidth(role: EventRole): string {
  return FILL[role.capacity ? Math.min(10, Math.round((role.confirmed / role.capacity) * 10)) : 0]!
}

type Action = 'join' | 'waitlist' | 'switch' | 'leave'

/** What the button of a role offers this member. */
function action(role: EventRole): Action | null {
  if (mine.value?.role_id === role.id) return 'leave'
  if (!open.value) return null
  if (mine.value) return 'switch'
  return full(role) ? 'waitlist' : 'join'
}

async function run(work: () => Promise<unknown>, failure: string) {
  if (busy.value) return
  busy.value = true
  try {
    await work()
  } catch (err) {
    toast.error(caughtErrorMessage(err, failure))
  } finally {
    busy.value = false
  }
}

function press(role: EventRole) {
  const what = action(role)
  if (what === 'leave') return run(() => eventsStore.leaveRole(props.event.id), t('events.roles.leaveFailed'))
  if (what) return run(() => eventsStore.signUp(props.event.id, role.id), t('events.roles.joinFailed'))
}

function remove(item: SignupItem) {
  return run(() => eventsStore.removeSignup(props.event.id, item.user.id), t('events.roles.removeFailed'))
}

const isMe = (item: SignupItem) => item.user.id === auth.user?.id
</script>

<template>
  <section v-if="roles.length > 0 || canManage" class="mb-6 rounded-xl border border-mnema-border bg-mnema-surface p-4" data-testid="event-roster">
    <div class="mb-3 flex items-center justify-between gap-3">
      <h3 class="text-sm font-medium text-mnema-text">{{ $t('events.roles.title') }}</h3>
      <button
        v-if="canManage && (open || roles.length > 0)"
        type="button"
        data-testid="event-roles-edit"
        class="flex items-center gap-1.5 rounded-lg border border-mnema-border px-2.5 py-1 text-xs font-medium text-mnema-secondary transition-colors hover:bg-mnema-hover max-md:min-h-11"
        @click="emit('edit-roles')"
      >
        <Pencil class="h-3.5 w-3.5" />
        {{ roles.length > 0 ? $t('events.roles.edit') : $t('events.roles.add') }}
      </button>
    </div>

    <p v-if="roles.length === 0" class="text-sm italic text-mnema-tertiary">{{ $t('events.roles.none') }}</p>

    <ul v-else class="grid gap-3 sm:grid-cols-2">
      <li v-for="role in roles" :key="role.id" data-testid="event-role" :data-role="role.name" class="flex min-w-0 flex-col gap-2 rounded-lg border border-mnema-hairline bg-mnema-canvas/60 p-3">
        <div class="flex items-baseline justify-between gap-2">
          <span class="min-w-0 truncate text-sm font-semibold text-mnema-text"><span v-if="role.emoji" class="mr-1">{{ role.emoji }}</span>{{ role.name }}</span>
          <span class="shrink-0 text-xs tabular-nums text-mnema-tertiary" data-testid="event-role-fill">{{ fill(role) }}</span>
        </div>
        <div v-if="role.capacity" class="h-1 overflow-hidden rounded-full bg-mnema-hairline" aria-hidden="true">
          <div :class="['h-full rounded-full', fillWidth(role), full(role) ? 'bg-amber-400' : 'bg-mnema-accent']"></div>
        </div>

        <ul v-if="members(role, 'confirmed').length" class="flex flex-col gap-1">
          <li v-for="item in members(role, 'confirmed')" :key="item.user.id" class="flex items-center justify-between gap-2 text-sm">
            <span class="flex min-w-0 items-center gap-2">
              <UserAvatar :user="eventAvatar(item.user)" size="sm" />
              <span class="truncate" :class="isMe(item) ? 'font-semibold text-mnema-text' : 'text-mnema-secondary'">{{ item.user.display_name || item.user.username }}</span>
            </span>
            <button v-if="canManage && open && !isMe(item)" type="button" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-mnema-tertiary hover:text-red-400 max-md:h-11 max-md:w-11" :aria-label="$t('events.roles.remove', { name: item.user.display_name || item.user.username })" @click="remove(item)">
              <X class="h-4 w-4" />
            </button>
          </li>
        </ul>

        <div v-if="members(role, 'waitlist').length" class="border-t border-mnema-hairline pt-2" data-testid="event-waitlist">
          <p class="mb-1 text-xs font-medium uppercase tracking-wide text-mnema-tertiary">{{ $t('events.roles.waitlistTitle', { count: role.waitlist }) }}</p>
          <ol class="flex flex-col gap-1">
            <li v-for="(item, i) in members(role, 'waitlist')" :key="item.user.id" class="flex items-center justify-between gap-2 text-sm text-mnema-tertiary">
              <span class="truncate"><span class="mr-1.5 tabular-nums">{{ i + 1 }}.</span>{{ item.user.display_name || item.user.username }}</span>
              <button v-if="canManage && open && !isMe(item)" type="button" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md hover:text-red-400 max-md:h-11 max-md:w-11" :aria-label="$t('events.roles.remove', { name: item.user.display_name || item.user.username })" @click="remove(item)">
                <X class="h-4 w-4" />
              </button>
            </li>
          </ol>
        </div>
        <p v-else-if="role.waitlist > 0" class="text-xs text-mnema-tertiary">{{ $t('events.roles.waitlistTitle', { count: role.waitlist }) }}</p>

        <p v-if="mine?.role_id === role.id && mine.state === 'waitlist'" class="rounded-md bg-amber-400/10 px-2 py-1 text-xs text-amber-300" data-testid="event-my-waitlist">
          {{ $t('events.roles.myPlace', { place: mine.waitlist_position }) }}
        </p>

        <button
          v-if="action(role)"
          type="button"
          data-testid="event-role-action"
          :disabled="busy"
          :class="[
            'mt-auto rounded-lg border px-3 py-1.5 text-sm font-medium transition-colors disabled:opacity-50 max-md:min-h-11',
            action(role) === 'leave' ? 'border-mnema-border text-mnema-secondary hover:bg-mnema-hover' : 'border-mnema-accent/40 text-mnema-accent hover:bg-mnema-accent/10'
          ]"
          @click="press(role)"
        >
          {{ $t(`events.roles.${action(role)}`) }}
        </button>
      </li>
    </ul>
  </section>
</template>
