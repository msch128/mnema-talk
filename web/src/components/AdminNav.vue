<script setup lang="ts">
// The admin console's navigation: the tabs grouped by what they are for. A
// quiet sidebar on a desktop, the full-width list of sections on a phone.
// The parent decides which tabs the member may use.
import { computed } from 'vue'
import type { Component } from 'vue'
import type { AdminTab } from '../lib/adminNav'
import { NAV_GROUPS } from '../lib/adminNav'
import { Users, FolderTree, Link, HardDrive, Server, Shield, ScrollText, Gavel, Bot, Sparkles, Smile, KeyRound, Flag, ShieldCheck, ShieldAlert, Activity, Download, ChevronRight } from '@lucide/vue'

const ICONS: Record<AdminTab, Component> = {
  users: Users, roles: Shield, channels: FolderTree, invites: Link, onboarding: Sparkles, access: KeyRound, emoji: Smile,
  reports: Flag, automod: ShieldCheck, raid: ShieldAlert, bans: Gavel, audit: ScrollText, bots: Bot, import: Download,
  media: HardDrive, system: Server, status: Activity
}

const props = defineProps<{
  tabs: readonly AdminTab[]
  active: AdminTab
  // 'list' is the phone's section list: larger rows with a chevron.
  list?: boolean
  // A short count shown beside a tab (open reports).
  badges?: Partial<Record<AdminTab, string>>
}>()
const emit = defineEmits<{ select: [tab: AdminTab] }>()

const groups = computed(() => NAV_GROUPS
  .map(group => ({ id: group.id, tabs: group.tabs.filter(tab => props.tabs.includes(tab)) }))
  .filter(group => group.tabs.length > 0))
</script>

<template>
  <nav
    data-testid="admin-tabs"
    :aria-label="$t('menu.adminConsole')"
    :class="list ? 'space-y-5 px-4 py-4' : 'space-y-4 px-3 py-4'"
  >
    <div v-for="group in groups" :key="group.id" :data-group="group.id" role="group" :aria-labelledby="`admin-nav-${group.id}`">
      <p :id="`admin-nav-${group.id}`" :class="['font-mono text-[11px] uppercase tracking-wider text-mnema-tertiary', list ? 'px-1 pb-1.5' : 'px-2.5 pb-1']">{{ $t(`admin.nav.${group.id}`) }}</p>
      <div :class="list ? 'overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface divide-y divide-mnema-hairline' : 'space-y-0.5'">
        <button
          v-for="tab in group.tabs"
          :key="tab"
          :data-testid="`tab-${tab}`"
          type="button"
          :aria-pressed="list ? undefined : active === tab ? 'true' : 'false'"
          :aria-current="active === tab ? 'page' : undefined"
          :class="list
            ? 'flex min-h-12 w-full items-center gap-3 px-4 text-left text-sm text-mnema-text transition hover:bg-mnema-hover'
            : ['flex min-h-8 w-full items-center gap-2.5 rounded-md px-2.5 py-1 text-left text-sm transition focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent', active === tab ? 'bg-mnema-accent/15 text-mnema-accent' : 'text-mnema-muted hover:bg-mnema-hover hover:text-mnema-text']"
          @click="emit('select', tab)"
        >
          <component :is="ICONS[tab]" :class="['h-4 w-4 shrink-0', list || active === tab ? 'text-mnema-accent' : '']" aria-hidden="true" />
          <span data-tab-label class="min-w-0 flex-1 truncate">{{ $t(`admin.tabs.${tab}`) }}</span>
          <span v-if="badges?.[tab]" :data-badge="tab" class="rounded-full bg-mnema-amber/20 px-1.5 text-xs font-semibold text-mnema-amber" :aria-label="$t('admin.nav.openReports', { count: badges[tab] ?? '' })">{{ badges[tab] }}</span>
          <ChevronRight v-if="list" class="h-4 w-4 shrink-0 text-mnema-tertiary" aria-hidden="true" />
        </button>
      </div>
    </div>
  </nav>
</template>
