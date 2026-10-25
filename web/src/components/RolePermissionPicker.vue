<script setup lang="ts">
// The permission switches of a role, grouped, with a search box that narrows
// them by name or explanation. Groups without a match disappear.
import { computed, ref } from 'vue'
import { Search } from '@lucide/vue'
import { PERMISSION_GROUPS } from '../lib/permissions'
import type { PermissionKey } from '../lib/permissions'
import { t } from '../i18n'
import PermissionToggles from './PermissionToggles.vue'

defineProps({
  // Bits the editor may not hand out (permissions they do not hold themselves).
  locked: { type: Number, default: 0 },
  readonly: { type: Boolean, default: false }
})
const allow = defineModel<number>('allow', { required: true })

const filter = ref('')
const groups = computed(() => {
  const needle = filter.value.trim().toLowerCase()
  return PERMISSION_GROUPS
    .map(group => ({
      ...group,
      keys: needle ? group.keys.filter((key: PermissionKey) => `${t(`perm.${key}.name`)} ${t(`perm.${key}.desc`)}`.toLowerCase().includes(needle)) : group.keys
    }))
    .filter(group => group.keys.length > 0)
})
</script>

<template>
  <div class="stack-2">
    <span class="text-xs font-semibold uppercase tracking-wide text-mnema-tertiary">{{ $t('roles.permissions') }}</span>
    <label class="relative block">
      <Search class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-mnema-tertiary" aria-hidden="true" />
      <input v-model="filter" type="search" :placeholder="$t('roles.searchPermissions')" :aria-label="$t('roles.searchPermissions')" data-testid="role-permission-search" class="w-full rounded-md border border-mnema-border-field bg-mnema-surface py-1.5 pl-8 pr-3 text-sm text-mnema-text outline-hidden transition focus:border-mnema-accent" />
    </label>
    <p v-if="!groups.length" class="py-2 text-sm text-mnema-tertiary" data-testid="role-permission-none">{{ $t('roles.noPermissionMatch') }}</p>
    <section v-for="group in groups" :key="group.id" class="stack-1.5" :data-permission-group="group.id">
      <h4 class="pt-1 text-xs font-medium text-mnema-muted">{{ $t(`perm.groups.${group.id}`) }}</h4>
      <PermissionToggles v-model:allow="allow" :keys="group.keys" :locked="locked" :readonly="readonly" />
    </section>
    <p v-if="locked && !readonly" class="text-xs text-mnema-tertiary">{{ $t('roles.cannotGrant') }}</p>
  </div>
</template>
