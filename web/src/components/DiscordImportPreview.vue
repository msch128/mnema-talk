<script setup lang="ts">
// The preview of a Discord import: the exact list of what would be created.
// Purely a view; nothing here writes anything.
import { computed } from 'vue'
import { Hash, Volume2, Lock } from '@lucide/vue'
import type { DiscordimportPlan, DiscordimportPlanChannel } from '../types/rest'
import { roleColorCss } from '../lib/permissions'
import { t } from '../i18n'

const props = defineProps<{ plan: DiscordimportPlan }>()

interface Group { key: string, name: string, channels: DiscordimportPlanChannel[] }

// Categories in creation order, each with its channels; the channels without
// a category come last, as they will in the layout.
const groups = computed<Group[]>(() => {
  const byCategory = new Map<string, DiscordimportPlanChannel[]>()
  for (const ch of props.plan.channels) {
    const list = byCategory.get(ch.category_source_id) ?? []
    list.push(ch)
    byCategory.set(ch.category_source_id, list)
  }
  const out: Group[] = props.plan.categories.map(c => ({ key: c.source_id, name: c.name, channels: byCategory.get(c.source_id) ?? [] }))
  const loose = byCategory.get('') ?? []
  if (loose.length) out.push({ key: '', name: t('discordImport.noCategory'), channels: loose })
  return out
})

function permName(key: string): string { return t(`perm.${key}.name`) }
</script>

<template>
  <div class="space-y-4" data-testid="import-preview">
    <div>
      <h4 class="text-sm font-semibold text-mnema-text">{{ $t('discordImport.previewTitle') }}</h4>
      <p class="text-xs text-mnema-tertiary">{{ plan.template_name || plan.source_name }} · {{ $t('discordImport.previewNote') }}</p>
    </div>

    <section class="rounded-lg border border-mnema-hairline bg-mnema-surface">
      <h5 class="border-b border-mnema-hairline px-4 py-2 text-xs font-mono uppercase tracking-wider text-mnema-muted">
        {{ $t('discordImport.structure') }} ·
        {{ $t('discordImport.counts.categories', { count: plan.counts.categories }) }},
        {{ $t('discordImport.counts.channels', { count: plan.counts.channels }) }}
      </h5>
      <div class="divide-y divide-mnema-hairline">
        <div v-for="group in groups" :key="group.key" class="px-4 py-2" data-testid="import-group">
          <div class="text-sm font-medium text-mnema-text">{{ group.name }}</div>
          <ul class="mt-1 space-y-0.5">
            <li v-for="ch in group.channels" :key="ch.source_id" class="flex flex-wrap items-center gap-x-2 text-sm text-mnema-muted" data-testid="import-channel">
              <Volume2 v-if="ch.type === 'voice'" class="h-3.5 w-3.5 shrink-0" :aria-label="$t('discordImport.typeVoice')" />
              <Hash v-else class="h-3.5 w-3.5 shrink-0" :aria-label="$t('discordImport.typeText')" />
              <span class="text-mnema-text">{{ ch.name }}</span>
              <Lock v-if="ch.private" class="h-3 w-3 shrink-0" :aria-label="$t('discordImport.private')" />
              <span v-if="ch.user_limit" class="text-xs text-mnema-tertiary">{{ $t('discordImport.limit', { n: ch.user_limit }) }}</span>
              <span v-for="note in ch.notes" :key="note" class="text-xs text-mnema-tertiary">· {{ note }}</span>
            </li>
          </ul>
        </div>
      </div>
    </section>

    <section class="rounded-lg border border-mnema-hairline bg-mnema-surface">
      <h5 class="border-b border-mnema-hairline px-4 py-2 text-xs font-mono uppercase tracking-wider text-mnema-muted">
        {{ $t('discordImport.rolesTitle') }} · {{ $t('discordImport.counts.roles', { count: plan.counts.roles }) }}
      </h5>
      <p class="px-4 pt-2 text-xs text-mnema-tertiary">{{ $t('discordImport.rolesHint') }}</p>
      <ul class="divide-y divide-mnema-hairline">
        <li v-for="role in plan.roles" :key="role.source_id" class="space-y-1 px-4 py-2 text-sm" data-testid="import-role">
          <div class="flex items-center gap-2">
            <span class="inline-block h-2.5 w-2.5 shrink-0 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }" aria-hidden="true"></span>
            <span class="font-medium text-mnema-text">{{ role.name }}</span>
          </div>
          <p class="text-xs text-mnema-muted">
            <template v-if="role.permission_keys.length">{{ role.permission_keys.map(permName).join(', ') }}</template>
            <template v-else>{{ $t('discordImport.noPermissions') }}</template>
          </p>
          <p v-if="role.dropped.length" class="text-xs text-mnema-tertiary">{{ $t('discordImport.notImported', { list: role.dropped.join(', ') }) }}</p>
        </li>
      </ul>
      <p v-if="plan.administrator_dropped" class="border-t border-mnema-hairline px-4 py-2 text-xs text-mnema-muted" data-testid="import-admin-dropped">{{ $t('discordImport.adminDropped') }}</p>
    </section>

    <section class="rounded-lg border border-mnema-hairline bg-mnema-surface px-4 py-3 text-xs text-mnema-tertiary">
      <div class="font-medium text-mnema-muted">{{ $t('discordImport.everyoneTitle') }}</div>
      <p>{{ $t('discordImport.everyoneBody', { list: plan.everyone.permission_keys.map(permName).join(', ') || $t('discordImport.noPermissions') }) }}</p>
    </section>

    <section v-if="plan.skipped.length" class="rounded-lg border border-mnema-hairline bg-mnema-surface px-4 py-3 text-xs text-mnema-tertiary" data-testid="import-skipped">
      <div class="font-medium text-mnema-muted">{{ $t('discordImport.skippedTitle') }}</div>
      <ul><li v-for="s in plan.skipped" :key="s">{{ s }}</li></ul>
    </section>
  </div>
</template>
