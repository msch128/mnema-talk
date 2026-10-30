<script setup lang="ts">
// The Moderation tab of a member's profile: what moderators and automod did
// about them, and the moderators' private notes. Only members with Manage
// reports get here; the server refuses everyone else, and the member
// themselves never sees any of it.
import { computed, onMounted, ref } from 'vue'
import { Loader2, Lock } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeModerationHistory, decodeModerationNote, type ModerationHistory, type ModerationHistoryEvent, type ModerationNote } from '../types/rest'
import { durationLabel } from '../lib/moderation'
import { confirm } from '../lib/confirm'
import { useToastStore } from '../stores/toast'
import { locale, t } from '../i18n'

const NOTE_MAX = 1000

const props = defineProps({
  userId: { type: String, required: true }
})
const emit = defineEmits<{ strikes: [count: number] }>()

const toasts = useToastStore()
const history = ref<ModerationHistory | null>(null)
const failed = ref(false)
const draft = ref('')
const draftStrike = ref(false)
const editingId = ref('')
const editText = ref('')
const editStrike = ref(false)
const busy = ref(false)

const base = computed(() => `/api/admin/users/${props.userId}/moderation`)
const when = (iso: string) => new Date(iso).toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'short' })

async function load() {
  try {
    history.value = await api(base.value, { decode: decodeModerationHistory })
    emit('strikes', history.value.strikes)
    failed.value = false
  } catch (error) {
    failed.value = true
    toasts.error(caughtErrorMessage(error, t('modHistory.loadFailed')))
  }
}

async function run(job: () => Promise<unknown>, failure: string) {
  if (busy.value) return false
  busy.value = true
  try {
    await job()
    await load()
    return true
  } catch (error) {
    toasts.error(caughtErrorMessage(error, failure))
    return false
  } finally {
    busy.value = false
  }
}

async function add() {
  const body = draft.value.trim()
  if (!body) return
  const json = { body, strike: draftStrike.value }
  if (await run(() => api(`${base.value}/notes`, { method: 'POST', json, decode: decodeModerationNote }), t('modHistory.saveFailed'))) {
    draft.value = ''
    draftStrike.value = false
  }
}

function startEdit(note: ModerationNote) {
  editingId.value = note.id
  editText.value = note.body
  editStrike.value = note.strike
}

async function saveEdit() {
  const body = editText.value.trim()
  if (!body) return
  const json = { body, strike: editStrike.value }
  if (await run(() => api(`${base.value}/notes/${editingId.value}`, { method: 'PUT', json, decode: decodeModerationNote }), t('modHistory.saveFailed'))) {
    editingId.value = ''
  }
}

async function remove(note: ModerationNote) {
  const ok = await confirm({
    title: t('modHistory.deleteTitle'),
    body: t('modHistory.deleteBody'),
    confirmLabel: t('modHistory.delete'),
    cancelLabel: t('common.cancel'),
    danger: true
  })
  if (ok) await run(() => api(`${base.value}/notes/${note.id}`, { method: 'DELETE' }), t('modHistory.deleteFailed'))
}

/** One line that says what happened; the reason, if any, is shown below it. */
function describe(ev: ModerationHistoryEvent): string {
  const actor = ev.actor || t('modHistory.someModerator')
  switch (ev.kind) {
    case 'timeout': return t('modHistory.event.timeout', { actor, duration: durationLabel(ev.duration_seconds ?? 0) })
    case 'ban': return ev.duration_seconds ? t('modHistory.event.banTimed', { actor, duration: durationLabel(ev.duration_seconds) }) : t('modHistory.event.ban', { actor })
    case 'roles': return t('modHistory.event.roles', { actor, changes: [...(ev.added_roles ?? []).map(r => `+${r}`), ...(ev.removed_roles ?? []).map(r => `−${r}`)].join(', ') })
    case 'report': return t('modHistory.event.report', { category: t(`reports.category.${ev.category ?? 'other'}`), status: t(`reports.admin.status.${ev.status ?? 'open'}`) })
    case 'automod': return t(`modHistory.event.automod.${ev.outcome ?? 'flagged'}`, { rule: ev.rule ?? '' }) + ((ev.hits ?? 1) > 1 ? ` ×${ev.hits}` : '')
    default: return t(`modHistory.event.${ev.kind}`, { actor })
  }
}

// The server names an erased author "Deleted member"; show it in the reader's language.
const authorOf = (note: ModerationNote) => (note.author_name === 'Deleted member' ? t('user.deleted') : note.author_name)

onMounted(load)
</script>

<template>
  <div class="space-y-4" data-testid="profile-moderation-tab">
    <p class="flex items-start gap-1.5 text-xs text-mnema-tertiary">
      <Lock class="mt-0.5 h-3 w-3 shrink-0" aria-hidden="true" /><span>{{ $t('modHistory.privacy') }}</span>
    </p>
    <p v-if="failed" role="alert" class="text-sm text-mnema-danger">{{ $t('modHistory.loadFailed') }}</p>
    <Loader2 v-else-if="!history" class="h-5 w-5 animate-spin text-mnema-accent" />

    <template v-else>
      <section :aria-label="$t('modHistory.notes')" class="space-y-2">
        <h4 class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('modHistory.notes') }}</h4>
        <form class="space-y-2" @submit.prevent="add">
          <label for="mod-note" class="sr-only">{{ $t('modHistory.noteLabel') }}</label>
          <textarea id="mod-note" v-model="draft" rows="2" v-max-chars="NOTE_MAX" :placeholder="$t('modHistory.notePlaceholder')" data-testid="mod-note-input" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"></textarea>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <label class="flex items-center gap-2 text-sm text-mnema-text">
              <input v-model="draftStrike" type="checkbox" data-testid="mod-note-strike" /><span>{{ $t('modHistory.asStrike') }}</span>
            </label>
            <button type="submit" :disabled="busy || !draft.trim()" class="btn btn-primary btn-sm" data-testid="mod-note-add">{{ $t('modHistory.add') }}</button>
          </div>
        </form>
        <p v-if="!history.notes.length" class="text-sm text-mnema-tertiary" data-testid="mod-notes-empty">{{ $t('modHistory.noNotes') }}</p>
        <ul v-else class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
          <li v-for="note in history.notes" :key="note.id" :data-note="note.id" class="space-y-1 px-3 py-2.5 text-sm">
            <template v-if="editingId === note.id">
              <textarea v-model="editText" rows="2" v-max-chars="NOTE_MAX" :aria-label="$t('modHistory.noteLabel')" data-testid="mod-note-edit-input" class="w-full rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"></textarea>
              <label class="flex items-center gap-2 text-sm text-mnema-text"><input v-model="editStrike" type="checkbox" /><span>{{ $t('modHistory.asStrike') }}</span></label>
              <div class="flex gap-2">
                <button type="button" :disabled="busy || !editText.trim()" class="btn btn-primary btn-sm" data-testid="mod-note-save" @click="saveEdit">{{ $t('common.save') }}</button>
                <button type="button" class="btn btn-ghost btn-sm" @click="editingId = ''">{{ $t('common.cancel') }}</button>
              </div>
            </template>
            <template v-else>
              <p class="flex flex-wrap items-center gap-2 text-xs text-mnema-tertiary">
                <span class="font-semibold text-mnema-text">{{ authorOf(note) }}</span>{{ when(note.created_at) }}
                <span v-if="note.strike" class="rounded-sm bg-mnema-danger/15 px-1.5 py-0.5 font-semibold text-mnema-danger" data-testid="mod-note-strike-badge">{{ $t('modHistory.strike') }}</span>
              </p>
              <p class="whitespace-pre-wrap wrap-break-word text-mnema-body-ink">{{ note.body }}</p>
              <div v-if="note.mine || note.can_delete" class="flex gap-3 text-xs">
                <button v-if="note.mine" type="button" class="text-mnema-accent hover:underline" data-testid="mod-note-edit" @click="startEdit(note)">{{ $t('modHistory.edit') }}</button>
                <button v-if="note.can_delete" type="button" class="text-mnema-danger hover:underline" data-testid="mod-note-delete" @click="remove(note)">{{ $t('modHistory.delete') }}</button>
              </div>
            </template>
          </li>
        </ul>
      </section>

      <section :aria-label="$t('modHistory.timeline')" class="space-y-2">
        <h4 class="text-xs font-bold uppercase tracking-wider text-mnema-tertiary">{{ $t('modHistory.timeline') }}</h4>
        <p v-if="!history.events.length" class="text-sm text-mnema-tertiary" data-testid="mod-events-empty">{{ $t('modHistory.noEvents') }}</p>
        <ol v-else class="space-y-1.5" data-testid="mod-events">
          <li v-for="(ev, i) in history.events" :key="i" :data-kind="ev.kind" class="rounded-md border border-mnema-hairline bg-mnema-surface px-3 py-2 text-sm">
            <p class="text-mnema-text">{{ describe(ev) }}</p>
            <p v-if="ev.reason" class="wrap-break-word text-xs text-mnema-muted">{{ $t('modHistory.reason', { reason: ev.reason }) }}</p>
            <p class="text-xs text-mnema-tertiary">{{ when(ev.at) }}</p>
          </li>
        </ol>
      </section>
    </template>
  </div>
</template>
