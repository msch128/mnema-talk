<script setup lang="ts">
// The three escalating confirmations before a Discord import runs. Each step
// says in plain words what happens; only the last one writes anything.
import { ref, computed } from 'vue'
import BaseDialog from './BaseDialog.vue'
import { applyImport, CONFIRM_WORD } from '../lib/discordImport'
import { isApiError, caughtErrorMessage } from '../lib/api'
import type { RoleMap } from '../lib/discordImport'
import type { DiscordimportPlan, DiscordimportRecord } from '../types/rest'
import { t } from '../i18n'

const props = defineProps<{ plan: DiscordimportPlan, template: string, roleMap?: RoleMap }>()
const emit = defineEmits<{ close: [], done: [record: DiscordimportRecord], stale: [] }>()

const step = ref<1 | 2 | 3>(1)
const typed = ref('')
const busy = ref(false)
const error = ref('')

const typedOk = computed(() => {
  const text = typed.value.trim()
  if (text === CONFIRM_WORD) return true
  return props.plan.server_name !== '' && text.toLowerCase() === props.plan.server_name.toLowerCase()
})
const step1Text = computed(() => t('discordImport.confirm.step1', {
  categories: t('discordImport.counts.categories', { count: props.plan.counts.categories }),
  channels: t('discordImport.counts.channels', { count: props.plan.counts.channels }),
  roles: t('discordImport.counts.roles', { count: props.plan.counts.roles })
}))
const step3Text = computed(() => props.plan.server_name
  ? t('discordImport.confirm.step3Named', { name: props.plan.server_name })
  : t('discordImport.confirm.step3'))

function permName(key: string): string { return t(`perm.${key}.name`) }

async function run() {
  if (!typedOk.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    emit('done', await applyImport(props.template, props.plan, typed.value.trim(), props.roleMap))
  } catch (e) {
    if (isApiError(e) && e.status === 409) {
      error.value = t('discordImport.confirm.stale')
      emit('stale')
    } else {
      error.value = caughtErrorMessage(e, t('discordImport.confirm.failed'))
    }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <BaseDialog
    :title="$t('discordImport.confirm.title')"
    :subtitle="$t('discordImport.confirm.step', { n: step })"
    panel-class="max-w-xl"
    :close-on-scrim="false"
    @close="emit('close')"
  >
    <div class="space-y-4 overflow-y-auto p-6" data-testid="import-confirm">
      <p v-if="step === 1" class="text-sm text-mnema-text" data-testid="import-step-1">{{ step1Text }}</p>

      <div v-else-if="step === 2" class="space-y-3" data-testid="import-step-2">
        <p class="text-sm text-mnema-text">{{ $t('discordImport.confirm.step2') }}</p>
        <ul class="max-h-56 space-y-1 overflow-y-auto rounded-lg border border-mnema-hairline bg-mnema-surface p-3 text-sm">
          <li v-for="role in plan.roles" :key="role.source_id">
            <span class="font-medium text-mnema-text">{{ role.name }}</span>
            <span class="text-mnema-muted">: {{ role.permission_keys.length ? role.permission_keys.map(permName).join(', ') : $t('discordImport.noPermissions') }}</span>
          </li>
        </ul>
        <ul v-if="plan.mapped_roles.length" class="space-y-1 rounded-lg border border-mnema-hairline bg-mnema-surface p-3 text-sm" data-testid="import-step-2-mapped">
          <li v-for="role in plan.mapped_roles" :key="role.source_id" class="text-mnema-muted">
            {{ $t('discordImport.confirm.step2Mapped', { source: role.source_name, name: role.role_name }) }}
          </li>
        </ul>
        <p v-if="plan.dropped_bits.length" class="text-xs text-mnema-muted" data-testid="import-dropped">{{ $t('discordImport.confirm.step2Dropped', { list: plan.dropped_bits.join(', ') }) }}</p>
        <p v-if="plan.administrator_dropped" class="text-xs text-mnema-muted">{{ $t('discordImport.confirm.step2AdminDropped') }}</p>
      </div>

      <div v-else class="space-y-3" data-testid="import-step-3">
        <p class="text-sm text-mnema-text">{{ step3Text }}</p>
        <input
          v-model="typed"
          type="text"
          autocomplete="off"
          data-testid="import-confirm-text"
          :aria-label="$t('discordImport.confirm.typeLabel')"
          class="w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-1.5 text-sm text-mnema-text outline-hidden transition focus:border-mnema-accent"
          @keydown.enter.prevent="run"
        />
      </div>

      <p v-if="error" class="text-sm text-mnema-danger" role="alert" data-testid="import-error">{{ error }}</p>
    </div>

    <footer class="flex items-center justify-between gap-3 border-t border-mnema-hairline px-6 py-4">
      <button v-if="step > 1" type="button" class="btn btn-secondary" :disabled="busy" data-testid="import-back" @click="step = step === 3 ? 2 : 1">{{ $t('discordImport.confirm.back') }}</button>
      <button v-else type="button" class="btn btn-secondary" data-testid="import-cancel" @click="emit('close')">{{ $t('common.cancel') }}</button>
      <button v-if="step === 1" type="button" class="btn btn-primary" data-testid="import-next" @click="step = 2">{{ $t('discordImport.confirm.step1Yes') }}</button>
      <button v-else-if="step === 2" type="button" class="btn btn-primary" data-testid="import-next" @click="step = 3">{{ $t('discordImport.confirm.step2Yes') }}</button>
      <button v-else type="button" class="btn btn-primary" :disabled="!typedOk || busy" data-testid="import-run" @click="run">
        {{ busy ? $t('discordImport.confirm.working') : $t('discordImport.confirm.go') }}
      </button>
    </footer>
  </BaseDialog>
</template>
