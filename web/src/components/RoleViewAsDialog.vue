<script setup lang="ts">
// "View server as <role>": a read-only preview of the sidebar for a made-up
// member who holds only @everyone and this role. The server computes it with
// the real permission engine and never shows more than the caller can see
// themselves. Nothing is impersonated and nothing here changes anything.
import { computed, onMounted, ref } from 'vue'
import { Check, Hash, Minus, Volume2 } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { CHANNEL_PERMISSION_KEYS } from '../lib/permissions'
import { decodeChatViewAsRole, type ChatViewAsChannel, type ChatViewAsRole } from '../types/rest'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const props = defineProps<{ roleId: string, roleName: string }>()
const emit = defineEmits<{ close: [] }>()

const preview = ref<ChatViewAsRole | null>(null)
const error = ref('')
const selectedId = ref('')

const channels = computed<ChatViewAsChannel[]>(() => preview.value
  ? [...preview.value.uncategorized, ...preview.value.categories.flatMap(c => c.channels)]
  : [])
const selected = computed(() => channels.value.find(c => c.id === selectedId.value) ?? null)
const empty = computed(() => !!preview.value && channels.value.length === 0 && preview.value.categories.length === 0)

onMounted(async () => {
  try {
    preview.value = await api(`/api/admin/roles/${props.roleId}/view-as`, { decode: decodeChatViewAsRole })
    selectedId.value = channels.value[0]?.id ?? ''
  } catch (err) {
    error.value = caughtErrorMessage(err, t('errors.unknown'))
  }
})
</script>

<template>
  <BaseDialog :title="$t('roles.viewAs.title', { name: roleName })" :subtitle="$t('roles.viewAs.subtitle')" panel-class="max-w-3xl" @close="emit('close')">
    <div class="min-h-0 overflow-y-auto p-6 max-md:p-4" data-testid="view-as">
      <div v-if="error" role="alert" class="rounded-sm border border-mnema-danger/30 bg-mnema-danger/10 p-2 text-sm text-mnema-danger">{{ error }}</div>
      <p v-else-if="!preview" role="status" class="text-sm text-mnema-tertiary">{{ $t('roles.viewAs.loading') }}</p>
      <template v-else>
        <p v-if="preview.administrator" class="mb-4 rounded-md border border-mnema-hairline bg-mnema-surface px-3 py-2 text-sm text-mnema-tertiary" data-testid="view-as-admin">{{ $t('roles.viewAs.administrator') }}</p>
        <p v-if="empty" class="text-sm text-mnema-tertiary" data-testid="view-as-empty">{{ $t('roles.viewAs.empty') }}</p>
        <div v-else class="grid gap-4 md:grid-cols-[minmax(0,16rem)_minmax(0,1fr)]">
          <!-- The sidebar as that role sees it -->
          <nav :aria-label="$t('roles.viewAs.sidebar')" class="space-y-3 rounded-lg border border-mnema-hairline bg-mnema-surface p-2" data-testid="view-as-sidebar">
            <template v-for="group in [{ id: '', name: '', channels: preview.uncategorized }, ...preview.categories]" :key="group.id">
              <p v-if="group.name" class="px-2 pt-1 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary" data-testid="view-as-category">{{ group.name }}</p>
              <ul class="space-y-0.5">
                <li v-for="ch in group.channels" :key="ch.id">
                  <button type="button" :aria-pressed="selectedId === ch.id" :data-channel="ch.name" :class="['flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm', selectedId === ch.id ? 'bg-mnema-accent-subtle text-mnema-text' : 'text-mnema-muted hover:bg-mnema-hover']" @click="selectedId = ch.id">
                    <Volume2 v-if="ch.type === 'voice'" class="h-4 w-4 shrink-0 text-mnema-tertiary" aria-hidden="true" />
                    <Hash v-else class="h-4 w-4 shrink-0 text-mnema-tertiary" aria-hidden="true" />
                    <span class="min-w-0 flex-1 truncate">{{ ch.name }}</span>
                    <span v-if="ch.type === 'voice'" :class="['shrink-0 text-xs', ch.can_join ? 'text-mnema-accent' : 'text-mnema-tertiary']" data-testid="view-as-join">{{ ch.can_join ? $t('roles.viewAs.canJoin') : $t('roles.viewAs.cannotJoin') }}</span>
                  </button>
                </li>
              </ul>
            </template>
          </nav>

          <!-- What the role may do in the selected channel -->
          <section v-if="selected" class="min-w-0 space-y-2" :aria-label="$t('roles.viewAs.permissionsIn', { name: selected.name })" data-testid="view-as-permissions">
            <h3 class="text-sm font-semibold text-mnema-text">{{ $t('roles.viewAs.permissionsIn', { name: selected.name }) }}</h3>
            <ul class="divide-y divide-mnema-hairline rounded-lg border border-mnema-hairline bg-mnema-surface">
              <li v-for="key in CHANNEL_PERMISSION_KEYS" :key="key" :data-permission="key" class="flex items-center gap-2 px-3 py-1.5 text-sm">
                <Check v-if="selected.permissions.includes(key)" class="h-4 w-4 shrink-0 text-mnema-accent" aria-hidden="true" />
                <Minus v-else class="h-4 w-4 shrink-0 text-mnema-tertiary" aria-hidden="true" />
                <span :class="selected.permissions.includes(key) ? 'text-mnema-text' : 'text-mnema-tertiary'">{{ $t(`perm.${key}.name`) }}</span>
                <span class="sr-only">{{ selected.permissions.includes(key) ? $t('roles.viewAs.allowed') : $t('roles.viewAs.notAllowed') }}</span>
              </li>
            </ul>
          </section>
        </div>
      </template>
      <div class="flex justify-end pt-4">
        <button type="button" class="btn btn-secondary" data-testid="view-as-exit" @click="emit('close')">{{ $t('roles.viewAs.exit') }}</button>
      </div>
    </div>
  </BaseDialog>
</template>
