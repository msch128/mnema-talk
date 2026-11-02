<script setup lang="ts">
// "Roles": the roles an admin opened to everyone. Choosing one is a personal
// preference, so it applies at once and says "Saved" (doc/settings-ux.md).
import { computed, onMounted, ref } from 'vue'
import { api } from '../lib/api'
import { arrayDecoder } from '../types/validation'
import { isPermRole } from '../types/rest'
import type { Role } from '../types/domain'
import { roleColorCss } from '../lib/permissions'
import { usePermissionsStore } from '../stores/permissions'
import { usePreferenceSave } from '../composables/usePreferenceSave'
import SavedIndicator from './SavedIndicator.vue'
import SwitchRow from './SwitchRow.vue'

const decodeRoles = arrayDecoder('roles', isPermRole)

const permissions = usePermissionsStore()
const pref = usePreferenceSave()
const roles = ref<Role[]>([])
const loaded = ref(false)
const failed = ref(false)
const busy = ref<string | null>(null)
const held = computed(() => new Set(permissions.myRoleIds))

async function load() {
  failed.value = false
  try {
    roles.value = await api('/api/roles/self-assignable', { decode: decodeRoles })
    loaded.value = true
  } catch {
    failed.value = true
  }
}

// The list as the server has it now; a reload that fails keeps what is shown and says so.
async function refresh() {
  await Promise.all([permissions.load(), load()])
}

async function toggle(role: Role, on: boolean) {
  busy.value = role.id
  try {
    const saved = await pref.run(() => api(`/api/me/roles/${role.id}`, { method: on ? 'PUT' : 'DELETE' }))
    // Whatever the outcome, show what the server holds now (the role may have been closed meanwhile).
    await refresh()
    return saved
  } finally {
    busy.value = null
  }
}

onMounted(load)
</script>

<template>
  <section class="settings-card" data-testid="self-roles" aria-labelledby="self-roles-title">
    <div class="flex items-center justify-between gap-3">
      <h3 id="self-roles-title" class="settings-title">{{ $t('selfRoles.title') }}</h3>
      <SavedIndicator :visible="pref.saved.value" />
    </div>
    <p class="settings-help">{{ $t('selfRoles.intro') }}</p>
    <div v-if="failed" class="space-y-2" role="alert">
      <p class="text-sm text-mnema-danger">{{ $t('selfRoles.loadFailed') }}</p>
      <button type="button" class="btn btn-ghost btn-sm" data-testid="self-roles-retry" @click="load">{{ $t('selfRoles.retry') }}</button>
    </div>
    <p v-if="loaded && !roles.length && !failed" class="text-sm text-mnema-tertiary" data-testid="self-roles-empty">{{ $t('selfRoles.empty') }}</p>
    <ul v-if="roles.length" class="divide-y divide-mnema-hairline">
      <li v-for="role in roles" :key="role.id" class="py-3" :data-role="role.name">
        <div class="flex items-start gap-3">
          <span v-if="role.color" class="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full" :style="{ background: roleColorCss(role.color) }" aria-hidden="true"></span>
          <SwitchRow
            class="min-w-0 flex-1"
            :model-value="held.has(role.id)"
            :title="role.name"
            :hint="role.description || $t('selfRoles.noDescription')"
            :disabled="busy !== null"
            :aria-label="$t('selfRoles.switch', { name: role.name })"
            :data-testid="`self-role-${role.id}`"
            @toggle="toggle(role, $event)"
          />
        </div>
      </li>
    </ul>
  </section>
</template>
