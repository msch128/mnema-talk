<script setup lang="ts">
// Admin dashboard: the automod rules. They check every new or edited message
// and every sign-up; a hit is blocked or only flagged and always lands in the
// reports queue. Needs the Administrator permission.
import { computed, onMounted, ref } from 'vue'
import { Plus, ShieldCheck, Loader2, Pencil, Trash2 } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeModerationAutomodList, type AutomodRule } from '../types/rest'
import type { ModerationAutomodList } from '../types/rest'
import { AUTOMOD_KINDS, ruleLabel, ruleSummary } from '../lib/automod'
import type { AutomodKind } from '../lib/automod'
import { confirm } from '../lib/confirm'
import { now, useServerClock } from '../lib/clock'
import { useToastStore } from '../stores/toast'
import { locale, t } from '../i18n'
import AutomodRuleDialog from './AutomodRuleDialog.vue'
import EmptyState from './EmptyState.vue'
import { useReloadOnActivate } from '../composables/useReloadOnActivate'

const toasts = useToastStore()
useServerClock()
const state = ref<ModerationAutomodList | null>(null)
const failed = ref(false)
const editing = ref<{ rule: AutomodRule | null; kind: AutomodKind } | null>(null)

const rules = computed(() => state.value?.rules ?? [])
// Raid protection exists once: it is offered again only after it was deleted.
const kinds = computed(() => AUTOMOD_KINDS.filter(k => k !== 'raid' || !rules.value.some(r => r.kind === 'raid')))
const raidUntil = computed(() => {
  const until = state.value?.raid_until
  return until && Date.parse(until) > now.value ? new Date(until).toLocaleTimeString(locale.value, { timeStyle: 'short' }) : ''
})

async function load() {
  try {
    state.value = await api('/api/admin/automod', { decode: decodeModerationAutomodList })
    failed.value = false
  } catch (error) {
    failed.value = true
    toasts.error(caughtErrorMessage(error, t('automod.loadFailed')))
  }
}

async function remove(rule: AutomodRule) {
  const ok = await confirm({
    title: t('automod.deleteTitle', { name: ruleLabel(rule) }),
    body: t('automod.deleteBody'),
    confirmLabel: t('automod.delete'),
    cancelLabel: t('common.cancel'),
    danger: true
  })
  if (!ok) return
  try {
    await api(`/api/admin/automod/rules/${rule.id}`, { method: 'DELETE' })
    await load()
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('automod.deleteFailed')))
  }
}

useReloadOnActivate(load)
onMounted(load)
</script>

<template>
  <section class="space-y-4" data-testid="admin-automod">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h3 class="flex items-center gap-2 font-mono text-sm font-semibold uppercase tracking-wider text-mnema-text">
        <ShieldCheck class="h-4 w-4 text-mnema-accent" /><span>{{ $t('automod.title') }}</span>
      </h3>
      <div class="flex flex-wrap gap-1.5" data-testid="automod-add">
        <button v-for="kind in kinds" :key="kind" type="button" :data-add="kind" class="btn btn-secondary btn-sm" @click="editing = { rule: null, kind }">
          <Plus class="h-3.5 w-3.5" />{{ $t(`automod.kind.${kind}`) }}
        </button>
      </div>
    </div>
    <p class="text-xs text-mnema-tertiary">{{ $t('automod.hint') }}</p>
    <p v-if="raidUntil" role="status" class="rounded-md border border-mnema-amber/40 bg-mnema-amber/10 px-3 py-2 text-sm text-mnema-text" data-testid="automod-raid-on">{{ $t('automod.raidOn', { time: raidUntil }) }}</p>

    <p v-if="failed" role="alert" class="text-sm text-mnema-danger">{{ $t('automod.loadFailed') }}</p>
    <Loader2 v-else-if="!state" class="h-5 w-5 animate-spin text-mnema-accent" />
    <ul v-else class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
      <li v-if="!rules.length" data-testid="automod-empty"><EmptyState :icon="ShieldCheck" :text="$t('automod.empty')" :hint="$t('automod.emptyHint')" /></li>
      <li v-for="rule in rules" :key="rule.id" :data-rule="rule.id" class="flex items-center gap-3 px-4 py-3">
        <div class="min-w-0 flex-1">
          <p class="flex flex-wrap items-center gap-2 text-sm font-semibold text-mnema-text">
            <span class="wrap-break-word">{{ ruleLabel(rule) }}</span>
            <span v-if="!rule.enabled" class="rounded-sm border border-mnema-hairline px-1.5 py-0.5 text-xs font-semibold text-mnema-tertiary" data-testid="rule-off">{{ $t('automod.off') }}</span>
            <span v-if="rule.kind !== 'raid'" class="rounded-sm bg-mnema-band px-1.5 py-0.5 text-xs font-semibold text-mnema-muted">{{ $t(`automod.actions.${rule.action}Short`) }}</span>
            <span v-if="rule.timeout_seconds > 0" class="rounded-sm bg-mnema-band px-1.5 py-0.5 text-xs font-semibold text-mnema-muted">{{ $t('automod.timeoutBadge') }}</span>
          </p>
          <p class="text-xs text-mnema-tertiary wrap-break-word" data-testid="rule-summary">{{ ruleSummary(rule) }}</p>
        </div>
        <button type="button" class="btn btn-ghost btn-sm max-md:min-h-11" :aria-label="$t('automod.edit', { name: ruleLabel(rule) })" data-testid="rule-edit" @click="editing = { rule, kind: rule.kind }"><Pencil class="h-4 w-4" /></button>
        <button type="button" class="btn btn-ghost btn-sm max-md:min-h-11 hover:text-mnema-danger" :aria-label="$t('automod.deleteRule', { name: ruleLabel(rule) })" data-testid="rule-delete" @click="remove(rule)"><Trash2 class="h-4 w-4" /></button>
      </li>
    </ul>

    <AutomodRuleDialog v-if="editing" :rule="editing.rule" :kind="editing.kind" @close="editing = null" @saved="load" />
  </section>
</template>
