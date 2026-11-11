<script setup lang="ts">
// The member's server-wide notification default: all messages, only mentions
// or nothing, a timed or open-ended mute of the whole server, and whether
// @all and @here are ignored. Channels and categories can override all of it
// (right-click them in the sidebar). Every choice is saved at once and says "Saved".
import { computed, ref, watch } from 'vue'
import { BellOff, Bell } from '@lucide/vue'
import BaseDialog from '../../shared/components/BaseDialog.vue'
import { useChatStore } from '../../stores/chat'
import { usePreferenceSave } from '../../shared/composables/usePreferenceSave'
import SavedIndicator from '../../shared/components/SavedIndicator.vue'
import { MUTE_DURATIONS, formatMuteEnd } from '../../chat/composables/useMuteMenu'
import { t } from '../../i18n'

const emit = defineEmits<{ close: [] }>()

const chatStore = useChatStore()
const pref = usePreferenceSave()

type ServerLevel = 'all' | 'mentions' | 'mute'
interface Pending { level: ServerLevel; muted_until: string | null; suppress_everyone: boolean }
const fromStore = (): Pending => {
  const s = chatStore.notifySettings.server
  return { level: s.level as ServerLevel, muted_until: s.muted_until ?? null, suppress_everyone: s.suppress_everyone }
}
// What the member has chosen so far. Every click builds on it and shows at once;
// the store only catches up after the PUT, so reading it would undo quick clicks.
const server = ref<Pending>(fromStore())
let inflight = 0
watch(() => chatStore.notifySettings.server, () => { if (inflight === 0) server.value = fromStore() }, { deep: true })
const muted = computed(() => server.value.level === 'mute')
const levels: Array<{ id: ServerLevel; label: string }> = [
  { id: 'all', label: t('notifications.all') },
  { id: 'mentions', label: t('notifications.mentions') },
  { id: 'mute', label: t('notifications.nothing') }
]
const muteStatus = computed(() =>
  server.value.muted_until
    ? t('notifications.muteServerStatus', { time: formatMuteEnd(server.value.muted_until) })
    : t('notifications.muteServerForever')
)

let queue: Promise<unknown> | null = null

async function persist(next: Pending) {
  const ok = await pref.run(async () => {
    if (!await chatStore.setServerNotification(next.level, next.muted_until, next.suppress_everyone)) throw new Error(t('notifications.serverSaveFailed'))
  })
  inflight--
  if (!ok && inflight === 0) server.value = fromStore()
}

// Saves run one after another; the first starts at once.
function save(next: Pending) {
  server.value = next
  inflight++
  const run = queue ? queue.then(() => persist(next)) : persist(next)
  queue = run
  void run.finally(() => { if (queue === run) queue = null })
}

function chooseLevel(level: ServerLevel) {
  save({ ...server.value, level, muted_until: null })
}

function muteFor(ms: number | null) {
  save({ ...server.value, level: 'mute', muted_until: ms === null ? null : new Date(Date.now() + ms).toISOString() })
}

function toggleSuppress() {
  save({ ...server.value, suppress_everyone: !server.value.suppress_everyone })
}

const rowClass = (on: boolean) => [
  'flex min-h-10 w-full items-center gap-3 rounded-lg border px-3 py-2 text-left text-sm transition-colors motion-reduce:transition-none focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent',
  on ? 'border-mnema-accent bg-mnema-accent/10 text-mnema-text' : 'border-mnema-hairline bg-mnema-surface text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text'
]
</script>

<template>
  <BaseDialog :title="$t('notifications.serverTitle')" :subtitle="$t('notifications.serverSubtitle')" panel-class="max-w-md select-none" @close="emit('close')">
    <div class="contents">
      <div class="p-6 space-y-6 overflow-y-auto max-h-[80vh]" data-testid="notify-dialog">
        <section class="space-y-2">
          <h3 class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">{{ $t('notifications.serverDefault') }}</h3>
          <div role="radiogroup" :aria-label="$t('notifications.serverDefault')" class="space-y-1.5">
            <button
              v-for="l in levels"
              :key="l.id"
              type="button"
              role="radio"
              :aria-checked="server.level === l.id ? 'true' : 'false'"
              :data-testid="`notify-server-${l.id}`"
              :class="rowClass(server.level === l.id)"
              @click="chooseLevel(l.id)"
            >{{ l.label }}</button>
          </div>
          <p class="settings-hint">{{ $t('notifications.serverDefaultHint') }}</p>
        </section>

        <section class="space-y-2">
          <h3 class="text-xs font-semibold text-mnema-tertiary uppercase tracking-wider font-mono">{{ $t('notifications.muteServer') }}</h3>
          <div class="grid grid-cols-2 gap-2">
            <button
              v-for="d in MUTE_DURATIONS.filter(x => x.id !== '15m')"
              :key="d.id"
              type="button"
              :class="['btn btn-secondary h-auto min-h-10 whitespace-normal text-center', d.ms === null && muted && !server.muted_until ? 'border-mnema-accent bg-mnema-accent/10' : '']"
              :aria-pressed="d.ms === null && muted && !server.muted_until ? 'true' : 'false'"
              :data-testid="`notify-server-mute-${d.id}`"
              @click="muteFor(d.ms)"
            >{{ $t(`notifications.mute_${d.id}`) }}</button>
          </div>
          <div v-if="muted" class="flex items-center justify-between gap-3 rounded-lg bg-mnema-surface border border-mnema-hairline px-3 py-2" data-testid="notify-server-status">
            <span class="flex min-w-0 items-center gap-2 text-sm text-mnema-text"><BellOff class="w-4 h-4 shrink-0 text-mnema-tertiary" /><span class="truncate">{{ muteStatus }}</span></span>
            <button type="button" class="btn btn-secondary" data-testid="notify-server-unmute" @click="chooseLevel('all')">
              <Bell class="w-4 h-4" />{{ $t('notifications.unmuteServer') }}
            </button>
          </div>
        </section>

        <section class="flex items-start justify-between gap-6">
          <div class="settings-row-text">
            <div class="settings-row-title">{{ $t('notifications.suppressEveryone') }}</div>
            <p class="settings-hint">{{ $t('notifications.suppressEveryoneHint') }}</p>
          </div>
          <button
            type="button"
            role="switch"
            :aria-checked="server.suppress_everyone ? 'true' : 'false'"
            :aria-label="$t('notifications.suppressEveryone')"
            class="switch mt-0.5"
            data-testid="notify-server-suppress"
            @click="toggleSuppress"
          ></button>
        </section>
      </div>
      <footer class="px-6 py-3.5 border-t border-mnema-hairline bg-mnema-raised flex items-center justify-between gap-3">
        <SavedIndicator :visible="pref.saved.value" />
        <button type="button" class="btn btn-primary" @click="emit('close')">{{ $t('common.close') }}</button>
      </footer>
    </div>
  </BaseDialog>
</template>
