<script setup lang="ts">
// Who may join a Talk and how many members fit. The fields belong to the Talk
// settings draft; nothing is written from here. Members who only lead a
// temporary Talk get the limit alone; the access choice needs Manage channels.
import { computed } from 'vue'
import type { AccessPreset } from '../lib/talkPresets'
import { usePermissionsStore } from '../stores/permissions'

defineProps({
  /** The member may change who can join (Manage channels). */
  canAccess: { type: Boolean, default: false },
  /** The current overwrites are not there (yet): the choice stays locked. */
  locked: { type: Boolean, default: false },
  rolesError: { type: String, default: '' },
  limitError: { type: String, default: '' }
})
const access = defineModel<AccessPreset>('access', { required: true })
const roles = defineModel<string[]>('roles', { required: true })
const limit = defineModel<number | string>('limit', { required: true })

const permissions = usePermissionsStore()
// Roles the editor may hand out: below their own rank.
const pickable = computed(() => permissions.roles.filter(r => !r.is_everyone && permissions.outranks(r)))

const option = 'flex items-start gap-3 rounded-lg border border-mnema-hairline bg-mnema-surface p-3 has-checked:border-mnema-accent has-checked:bg-mnema-accent-subtle'
const legend = 'mb-1.5 font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary'
</script>

<template>
  <div class="space-y-5">
    <fieldset v-if="canAccess" class="space-y-2" data-testid="talk-presets" :disabled="locked">
      <legend :class="legend">{{ $t('talk.presetsTitle') }}</legend>
      <label :class="option">
        <input v-model="access" type="radio" value="everyone" name="talk-preset" class="mt-1 accent-mnema-accent" data-testid="preset-everyone" />
        <span><span class="block text-sm font-medium text-mnema-text">{{ $t('talk.presetEveryone') }}</span><span class="text-xs text-mnema-tertiary">{{ $t('talk.presetEveryoneHint') }}</span></span>
      </label>
      <label :class="option">
        <input v-model="access" type="radio" value="roles" name="talk-preset" class="mt-1 accent-mnema-accent" data-testid="preset-roles" />
        <span class="min-w-0">
          <span class="block text-sm font-medium text-mnema-text">{{ $t('talk.presetRoles') }}</span>
          <span class="text-xs text-mnema-tertiary">{{ $t('talk.presetRolesHint') }}</span>
          <span v-if="access === 'roles'" class="mt-2 flex flex-wrap gap-x-4 gap-y-1" data-testid="preset-role-list">
            <label v-for="role in pickable" :key="role.id" class="flex items-center gap-1.5 text-sm text-mnema-text">
              <input v-model="roles" type="checkbox" :value="role.id" class="accent-mnema-accent" />{{ role.name }}
            </label>
            <span v-if="!pickable.length" class="text-xs text-mnema-tertiary">{{ $t('talk.presetNoRoles') }}</span>
          </span>
          <span v-if="access === 'roles' && rolesError" class="mt-2 block text-xs text-mnema-danger" data-testid="preset-roles-error">{{ rolesError }}</span>
        </span>
      </label>
    </fieldset>

    <div class="space-y-1.5" data-testid="talk-limit">
      <label for="talk-limit-input" :class="[legend, 'block']">{{ $t('talk.presetLimit') }}</label>
      <input
        id="talk-limit-input"
        v-model.number="limit"
        type="number"
        min="0"
        max="999"
        :aria-invalid="limitError ? 'true' : undefined"
        data-testid="preset-limit-input"
        class="block w-28 rounded-md border border-mnema-border-field bg-mnema-canvas px-3 py-2 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
      />
      <p class="text-xs text-mnema-tertiary">{{ $t('talk.presetLimitHint') }}</p>
      <p v-if="limitError" class="text-xs text-mnema-danger" data-testid="preset-limit-error">{{ limitError }}</p>
    </div>
  </div>
</template>
