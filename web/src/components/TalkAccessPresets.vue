<script setup lang="ts">
// Quick presets for who may join a Talk. They write the Talk's ordinary
// permission overwrites and member limit, nothing else. Members who only lead a
// temporary Talk get the limit alone; the access presets need Manage channels.
import type { PropType } from 'vue'
import { computed, onMounted, ref } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeOverwritesBody } from '../types/domain'
import type { Channel, Overwrite } from '../types/domain'
import { currentPreset, presetEveryone, presetOnlyRoles, rolesLetIn } from '../lib/talkPresets'
import { updateTalk } from '../lib/talks'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const props = defineProps({
  channel: { type: Object as PropType<Channel>, required: true },
  /** The member may change who can join (Manage channels). */
  canAccess: { type: Boolean, default: false }
})
const permissions = usePermissionsStore()
const toasts = useToastStore()

type Choice = 'everyone' | 'roles' | 'limit'
const choice = ref<Choice>(props.canAccess ? 'everyone' : 'limit')
const chosenRoles = ref<string[]>([])
const limit = ref(props.channel.user_limit || 0)
const entries = ref<Overwrite[]>([])
const loaded = ref(!props.canAccess)
const busy = ref(false)
// What is stored now: the form only has something to save once it differs.
const snapshot = () => JSON.stringify([choice.value, [...chosenRoles.value].sort(), limit.value])
const baseline = ref(snapshot())
const dirty = computed(() => loaded.value && snapshot() !== baseline.value)

const everyoneId = computed(() => permissions.roles.find(r => r.is_everyone)?.id ?? '')
const base = computed(() => `/api/admin/channels/${props.channel.id}/overwrites`)
// Roles the editor may hand out: below their own rank.
const pickable = computed(() => permissions.roles.filter(r => !r.is_everyone && permissions.outranks(r)))
const needsRole = computed(() => choice.value === 'roles' && chosenRoles.value.length === 0)

async function load() {
  if (!props.canAccess) return
  try {
    entries.value = (await api(base.value, { decode: decodeOverwritesBody })).overwrites
    choice.value = currentPreset(entries.value, everyoneId.value)
    chosenRoles.value = rolesLetIn(entries.value, everyoneId.value)
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('overwrites.loadFailed')))
  } finally {
    loaded.value = true
    baseline.value = snapshot()
  }
}

async function writeAccess() {
  const next = choice.value === 'roles'
    ? presetOnlyRoles(entries.value, everyoneId.value, chosenRoles.value)
    : presetEveryone(entries.value, everyoneId.value)
  await api(base.value, { method: 'PUT', json: { overwrites: next } })
  entries.value = next
}

// Writes the choice; the dialog's one Save button calls it and reports the result.
async function save() {
  if (busy.value || needsRole.value || !dirty.value) return
  busy.value = true
  try {
    if (choice.value === 'limit') await updateTalk(props.channel.id, { user_limit: Math.max(0, Math.min(999, Math.trunc(limit.value) || 0)) }, permissions.can('manage_channels'))
    else await writeAccess()
    await permissions.load()
    baseline.value = snapshot()
  } finally {
    busy.value = false
  }
}
defineExpose({ save, canSave: computed(() => dirty.value && !needsRole.value) })

onMounted(load)

const option = 'flex items-start gap-3 rounded-lg border border-mnema-hairline bg-mnema-surface p-3 has-checked:border-mnema-accent has-checked:bg-mnema-accent-subtle'
</script>

<template>
  <fieldset class="space-y-2" data-testid="talk-presets" :disabled="!loaded || busy">
    <legend class="mb-1.5 font-mono text-xs font-medium uppercase tracking-wider text-mnema-tertiary">{{ canAccess ? $t('talk.presetsTitle') : $t('talk.presetLimit') }}</legend>

    <label v-if="canAccess" :class="option">
      <input v-model="choice" type="radio" value="everyone" name="talk-preset" class="mt-1 accent-mnema-accent" data-testid="preset-everyone" />
      <span><span class="block text-sm font-medium text-mnema-text">{{ $t('talk.presetEveryone') }}</span><span class="text-xs text-mnema-tertiary">{{ $t('talk.presetEveryoneHint') }}</span></span>
    </label>

    <label v-if="canAccess" :class="option">
      <input v-model="choice" type="radio" value="roles" name="talk-preset" class="mt-1 accent-mnema-accent" data-testid="preset-roles" />
      <span class="min-w-0">
        <span class="block text-sm font-medium text-mnema-text">{{ $t('talk.presetRoles') }}</span>
        <span class="text-xs text-mnema-tertiary">{{ $t('talk.presetRolesHint') }}</span>
        <span v-if="choice === 'roles'" class="mt-2 flex flex-wrap gap-x-4 gap-y-1" data-testid="preset-role-list">
          <label v-for="role in pickable" :key="role.id" class="flex items-center gap-1.5 text-sm text-mnema-text">
            <input v-model="chosenRoles" type="checkbox" :value="role.id" class="accent-mnema-accent" />{{ role.name }}
          </label>
          <span v-if="!pickable.length" class="text-xs text-mnema-tertiary">{{ $t('talk.presetNoRoles') }}</span>
        </span>
      </span>
    </label>

    <label :class="option">
      <input v-if="canAccess" v-model="choice" type="radio" value="limit" name="talk-preset" class="mt-1 accent-mnema-accent" data-testid="preset-limit" />
      <span>
        <span v-if="canAccess" class="block text-sm font-medium text-mnema-text">{{ $t('talk.presetLimit') }}</span>
        <span class="text-xs text-mnema-tertiary">{{ $t('talk.presetLimitHint') }}</span>
        <input
          v-if="choice === 'limit'"
          v-model.number="limit"
          type="number"
          min="0"
          max="999"
          :aria-label="$t('talk.limitLabel')"
          data-testid="preset-limit-input"
          class="mt-2 block w-24 rounded-md border border-mnema-border-field bg-mnema-canvas px-2 py-1 text-sm text-mnema-text focus:border-mnema-accent focus:outline-hidden"
        />
      </span>
    </label>

  </fieldset>
</template>
