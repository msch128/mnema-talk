<script setup lang="ts">
// Admin dashboard: import the structure of a Discord server from a template
// link. Paste, preview (writes nothing), confirm three times, import; each
// import can be undone later.
import { ref, onMounted } from 'vue'
import { Download, Undo2 } from '@lucide/vue'
import { caughtErrorMessage } from '../lib/api'
import { confirm } from '../lib/confirm'
import { previewImport, listImports, importDetail, undoImport } from '../lib/discordImport'
import type { RoleMap } from '../lib/discordImport'
import type { DiscordimportPlan, DiscordimportPlanRole, DiscordimportRecord, DiscordimportCounts } from '../types/rest'
import DiscordImportPreview from './DiscordImportPreview.vue'
import DiscordImportConfirm from './DiscordImportConfirm.vue'
import EmptyState from './EmptyState.vue'
import { useToastStore } from '../stores/toast'
import { t, locale } from '../i18n'

const toasts = useToastStore()

const link = ref('')
const plan = ref<DiscordimportPlan | null>(null)
// The link the preview was made for: the import repeats exactly this one.
const previewed = ref('')
// The Discord roles to choose for, the choices, and the choices the shown plan
// was made with: changing a choice re-plans when the import is reviewed.
const sourceRoles = ref<DiscordimportPlanRole[]>([])
const roleMap = ref<RoleMap>({})
const plannedMap = ref<RoleMap>({})
const busy = ref(false)
const confirming = ref(false)
const imports = ref<DiscordimportRecord[]>([])

function when(iso: string): string {
  return new Date(iso).toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'short' })
}

function summary(c: DiscordimportCounts): string {
  return [
    t('discordImport.counts.categories', { count: c.categories }),
    t('discordImport.counts.channels', { count: c.channels }),
    t('discordImport.counts.roles', { count: c.roles })
  ].join(', ')
}

async function loadHistory() {
  try {
    imports.value = await listImports()
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  }
}

// A role with an existing role of the same name starts out joined to it.
function suggestedMap(roles: DiscordimportPlanRole[]): RoleMap {
  const out: RoleMap = {}
  for (const r of roles) {
    if (r.suggested_role_id) out[r.source_id] = r.suggested_role_id
  }
  return out
}

function sameMap(a: RoleMap, b: RoleMap): boolean {
  const keys = Object.keys(a)
  return keys.length === Object.keys(b).length && keys.every(k => a[k] === b[k])
}

async function preview() {
  const value = link.value.trim()
  if (!value || busy.value) return
  busy.value = true
  plan.value = null
  try {
    const first = await previewImport(value)
    const picks = suggestedMap(first.roles)
    plan.value = Object.keys(picks).length ? await previewImport(value, picks) : first
    sourceRoles.value = first.roles
    roleMap.value = plannedMap.value = picks
    previewed.value = value
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('discordImport.previewFailed')))
  } finally {
    busy.value = false
  }
}

// Plans again with the chosen roles, then asks for the confirmations.
async function review() {
  if (busy.value) return
  if (!sameMap(roleMap.value, plannedMap.value)) {
    busy.value = true
    try {
      plan.value = await previewImport(previewed.value, roleMap.value)
      plannedMap.value = { ...roleMap.value }
    } catch (e) {
      toasts.error(caughtErrorMessage(e, t('discordImport.previewFailed')))
      return
    } finally {
      busy.value = false
    }
  }
  confirming.value = true
}

async function onImported(record: DiscordimportRecord) {
  confirming.value = false
  plan.value = null
  link.value = ''
  toasts.success(t('discordImport.confirm.done', { summary: summary(record.counts) }))
  await loadHistory()
}

// The server refused the confirmed plan because things changed: start over.
function onStale() {
  confirming.value = false
  plan.value = null
  toasts.error(t('discordImport.confirm.stale'))
}

async function undo(record: DiscordimportRecord) {
  try {
    const detail = await importDetail(record.id)
    const kept = detail.kept_categories.length ? t('discordImport.undo.kept', { list: detail.kept_categories.join(', ') }) : ''
    const ok = await confirm({
      title: t('discordImport.undo.title'),
      body: t('discordImport.undo.body') + kept,
      confirmLabel: t('discordImport.undo.confirm'),
      danger: true
    })
    if (!ok) return
    // Channels that have messages now need their own, explicit confirmation.
    const withMessages = detail.channels_with_messages
    if (withMessages.length) {
      const sure = await confirm({
        title: t('discordImport.undo.messagesTitle'),
        body: t('discordImport.undo.messagesBody', { list: withMessages.map(c => `${c.name} (${c.messages})`).join(', ') }),
        confirmLabel: t('discordImport.undo.messagesConfirm'),
        danger: true
      })
      if (!sure) return
    }
    const result = await undoImport(record.id, withMessages.length > 0)
    toasts.success(t('discordImport.undo.done', { summary: summary(result.removed) })
      + (result.kept_roles ? t('discordImport.undo.keptRoles', { kept: result.kept_roles }) : ''))
    await loadHistory()
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('discordImport.undo.failed')))
  }
}

onMounted(() => { void loadHistory() })
</script>

<template>
  <section class="space-y-5">
    <h3 class="flex items-center gap-2 text-sm font-semibold uppercase tracking-wider text-mnema-text font-mono">
      <Download class="h-4 w-4 text-mnema-accent" />
      <span>{{ $t('discordImport.title') }}</span>
    </h3>
    <p class="text-sm text-mnema-muted">{{ $t('discordImport.intro') }}</p>

    <form class="space-y-2" @submit.prevent="preview">
      <label class="block text-xs text-mnema-tertiary" for="import-link">{{ $t('discordImport.linkLabel') }}</label>
      <div class="flex flex-wrap gap-2">
        <input
          id="import-link"
          v-model="link"
          type="text"
          autocomplete="off"
          spellcheck="false"
          maxlength="300"
          data-testid="import-link"
          :placeholder="$t('discordImport.linkPlaceholder')"
          class="min-w-0 flex-1 basis-64 rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text placeholder-mnema-tertiary outline-hidden transition focus:border-mnema-accent"
        />
        <button type="submit" class="btn btn-primary" :disabled="!link.trim() || busy" data-testid="import-preview-button">
          {{ busy ? $t('discordImport.previewing') : $t('discordImport.preview') }}
        </button>
      </div>
      <p class="text-xs text-mnema-tertiary">{{ $t('discordImport.howTo') }}</p>
    </form>

    <template v-if="plan">
      <DiscordImportPreview v-model:role-map="roleMap" :plan="plan" :roles="sourceRoles" />
      <button type="button" class="btn btn-primary" :disabled="busy" data-testid="import-review" @click="review">{{ $t('discordImport.review') }}</button>
    </template>

    <DiscordImportConfirm
      v-if="plan && confirming"
      :plan="plan"
      :template="previewed"
      :role-map="plannedMap"
      @close="confirming = false"
      @done="onImported"
      @stale="onStale"
    />

    <div class="space-y-2">
      <h4 class="text-xs font-mono uppercase tracking-wider text-mnema-muted">{{ $t('discordImport.history.title') }}</h4>
      <div class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
        <EmptyState v-if="!imports.length" :icon="Download" :text="$t('discordImport.history.empty')" :hint="$t('discordImport.history.emptyHint')" data-testid="import-history-empty" />
        <div v-for="rec in imports" :key="rec.id" class="flex items-center justify-between gap-3 px-4 py-2 text-sm" data-testid="import-record">
          <div class="min-w-0">
            <div class="truncate text-mnema-text">{{ $t('discordImport.history.row', { name: rec.template_name || rec.template_code, when: when(rec.created_at) }) }}</div>
            <div class="text-xs text-mnema-tertiary">{{ summary(rec.counts) }}<template v-if="rec.undone_at"> · {{ $t('discordImport.history.undone', { when: when(rec.undone_at) }) }}</template></div>
          </div>
          <button
            v-if="!rec.undone_at"
            type="button"
            class="btn btn-secondary shrink-0"
            data-testid="import-undo"
            @click="undo(rec)"
          >
            <Undo2 class="h-4 w-4" />
            <span>{{ $t('discordImport.history.undo') }}</span>
          </button>
        </div>
      </div>
    </div>
  </section>
</template>
