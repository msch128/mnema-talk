<script setup lang="ts">
// Create or edit one automod rule. The server validates everything again; the
// form only keeps obvious mistakes from being sent.
import type { PropType } from 'vue'
import type { AutomodRule } from '../types/rest'
import { computed, ref } from 'vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeAutomodRule } from '../types/rest'
import {
  ACCOUNT_AGES, RAID_RESTRICTIONS, RAID_WINDOWS, RULE_TIMEOUTS, MAX_RULE_NAME,
  emptyForm, formFromRule, formProblem, inputFromForm, withCurrent
} from '../lib/automod'
import type { AutomodKind } from '../lib/automod'
import { durationLabel } from '../lib/moderation'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import BaseDialog from './BaseDialog.vue'

const props = defineProps({
  rule: { type: Object as PropType<AutomodRule | null>, default: null },
  kind: { type: String as PropType<AutomodKind>, default: 'words' }
})
const emit = defineEmits<{ close: []; saved: [] }>()

const toasts = useToastStore()
const chat = useChatStore()
const permissions = usePermissionsStore()

const form = ref(props.rule ? formFromRule(props.rule) : emptyForm(props.kind))
const busy = ref(false)
const problem = computed(() => formProblem(form.value))
const roles = computed(() => permissions.roles.filter(r => !r.is_everyone && !r.managed))
const channels = computed(() => chat.allChannels)
const isRaid = computed(() => form.value.kind === 'raid')

function toggle(list: string[], id: string) {
  const at = list.indexOf(id)
  if (at >= 0) list.splice(at, 1)
  else list.push(id)
}

async function save() {
  if (problem.value || busy.value) return
  busy.value = true
  try {
    const path = props.rule ? `/api/admin/automod/rules/${props.rule.id}` : '/api/admin/automod/rules'
    await api(path, { method: props.rule ? 'PUT' : 'POST', json: inputFromForm(form.value), decode: decodeAutomodRule })
    toasts.success(t('automod.saved'))
    emit('saved')
    emit('close')
  } catch (error) {
    toasts.error(caughtErrorMessage(error, t('automod.saveFailed')))
  } finally {
    busy.value = false
  }
}

const field = 'w-full rounded-md border border-mnema-border-field bg-mnema-surface px-3 py-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent'
const label = 'block text-xs font-medium text-mnema-muted font-mono uppercase tracking-wider'
</script>

<template>
  <BaseDialog :title="rule ? $t('automod.editTitle') : $t('automod.newTitle', { kind: $t(`automod.kind.${form.kind}`) })" panel-class="max-w-xl" @close="emit('close')">
    <form class="space-y-4 overflow-y-auto p-5" data-testid="automod-form" @submit.prevent="save">
      <p class="text-sm text-mnema-tertiary">{{ $t(`automod.about.${form.kind}`) }}</p>

      <div class="space-y-1">
        <label for="am-name" :class="label">{{ $t('automod.name') }}</label>
        <input id="am-name" v-model="form.name" type="text" :maxlength="MAX_RULE_NAME" :class="field" data-testid="am-name" />
        <p class="text-xs text-mnema-tertiary">{{ $t('automod.nameHint') }}</p>
      </div>

      <div v-if="form.kind === 'words'" class="space-y-1">
        <label for="am-words" :class="label">{{ $t('automod.words') }}</label>
        <textarea id="am-words" v-model="form.words" rows="6" :class="field" data-testid="am-words"></textarea>
        <p class="text-xs text-mnema-tertiary">{{ $t('automod.wordsHint') }}</p>
      </div>

      <template v-if="form.kind === 'links'">
        <div class="space-y-1">
          <label for="am-mode" :class="label">{{ $t('automod.linkMode') }}</label>
          <select id="am-mode" v-model="form.mode" :class="field" data-testid="am-mode">
            <option value="block_all">{{ $t('automod.linkModes.block_all') }}</option>
            <option value="allow_list">{{ $t('automod.linkModes.allow_list') }}</option>
            <option value="block_list">{{ $t('automod.linkModes.block_list') }}</option>
          </select>
        </div>
        <div v-if="form.mode !== 'block_all'" class="space-y-1">
          <label for="am-domains" :class="label">{{ $t('automod.domains') }}</label>
          <textarea id="am-domains" v-model="form.domains" rows="5" :class="field" data-testid="am-domains"></textarea>
          <p class="text-xs text-mnema-tertiary">{{ $t('automod.domainsHint') }}</p>
        </div>
      </template>

      <div v-if="form.kind === 'mentions'" class="space-y-1">
        <label for="am-mentions" :class="label">{{ $t('automod.maxMentions') }}</label>
        <input id="am-mentions" v-model.number="form.maxMentions" type="number" min="1" max="50" :class="field" data-testid="am-mentions" />
        <p class="text-xs text-mnema-tertiary">{{ $t('automod.maxMentionsHint') }}</p>
      </div>

      <template v-if="form.kind === 'new_account'">
        <div class="space-y-1">
          <label for="am-age" :class="label">{{ $t('automod.accountAge') }}</label>
          <select id="am-age" v-model.number="form.maxAgeSeconds" :class="field" data-testid="am-age">
            <option v-for="s in withCurrent(ACCOUNT_AGES, form.maxAgeSeconds)" :key="s" :value="s">{{ durationLabel(s) }}</option>
          </select>
        </div>
        <div class="space-y-1">
          <label for="am-restrict" :class="label">{{ $t('automod.restrict') }}</label>
          <select id="am-restrict" v-model="form.restrict" :class="field" data-testid="am-restrict">
            <option value="links">{{ $t('automod.restricts.links') }}</option>
            <option value="attachments">{{ $t('automod.restricts.attachments') }}</option>
            <option value="links_and_attachments">{{ $t('automod.restricts.links_and_attachments') }}</option>
            <option value="everything">{{ $t('automod.restricts.everything') }}</option>
          </select>
        </div>
      </template>

      <template v-if="isRaid">
        <div class="grid gap-3 sm:grid-cols-3">
          <div class="space-y-1">
            <label for="am-joins" :class="label">{{ $t('automod.joins') }}</label>
            <input id="am-joins" v-model.number="form.joins" type="number" min="2" max="200" :class="field" data-testid="am-joins" />
          </div>
          <div class="space-y-1">
            <label for="am-window" :class="label">{{ $t('automod.window') }}</label>
            <select id="am-window" v-model.number="form.windowSeconds" :class="field" data-testid="am-window">
              <option v-for="s in withCurrent(RAID_WINDOWS, form.windowSeconds)" :key="s" :value="s">{{ durationLabel(s) }}</option>
            </select>
          </div>
          <div class="space-y-1">
            <label for="am-restrict-time" :class="label">{{ $t('automod.restrictTime') }}</label>
            <select id="am-restrict-time" v-model.number="form.restrictSeconds" :class="field" data-testid="am-restrict-time">
              <option v-for="s in withCurrent(RAID_RESTRICTIONS, form.restrictSeconds)" :key="s" :value="s">{{ durationLabel(s) }}</option>
            </select>
          </div>
        </div>
        <p class="text-xs text-mnema-tertiary">{{ $t('automod.raidHint') }}</p>
      </template>

      <template v-else>
        <fieldset class="space-y-1">
          <legend :class="label">{{ $t('automod.action') }}</legend>
          <label class="flex items-center gap-2 text-sm text-mnema-text">
            <input v-model="form.action" type="radio" value="block" data-testid="am-action-block" /><span>{{ $t('automod.actions.block') }}</span>
          </label>
          <label class="flex items-center gap-2 text-sm text-mnema-text">
            <input v-model="form.action" type="radio" value="flag" data-testid="am-action-flag" /><span>{{ $t('automod.actions.flag') }}</span>
          </label>
        </fieldset>
        <div class="space-y-1">
          <label for="am-timeout" :class="label">{{ $t('automod.timeout') }}</label>
          <select id="am-timeout" v-model.number="form.timeoutSeconds" :class="field" data-testid="am-timeout">
            <option v-for="s in withCurrent(RULE_TIMEOUTS, form.timeoutSeconds)" :key="s" :value="s">{{ s === 0 ? $t('automod.noTimeout') : durationLabel(s) }}</option>
          </select>
        </div>
      </template>

      <fieldset class="space-y-1">
        <legend :class="label">{{ $t('automod.exemptRoles') }}</legend>
        <p v-if="!roles.length" class="text-xs text-mnema-tertiary">{{ $t('automod.noRoles') }}</p>
        <div class="flex flex-wrap gap-x-4 gap-y-1">
          <label v-for="role in roles" :key="role.id" class="flex items-center gap-2 text-sm text-mnema-text">
            <input type="checkbox" :checked="form.exemptRoles.includes(role.id)" :data-exempt-role="role.name" @change="toggle(form.exemptRoles, role.id)" /><span>{{ role.name }}</span>
          </label>
        </div>
      </fieldset>

      <fieldset class="space-y-1">
        <legend :class="label">{{ $t('automod.exemptChannels') }}</legend>
        <div class="flex max-h-32 flex-wrap gap-x-4 gap-y-1 overflow-y-auto">
          <label v-for="channel in channels" :key="channel.id" class="flex items-center gap-2 text-sm text-mnema-text">
            <input type="checkbox" :checked="form.exemptChannels.includes(channel.id)" :data-exempt-channel="channel.name" @change="toggle(form.exemptChannels, channel.id)" /><span># {{ channel.name }}</span>
          </label>
        </div>
        <p class="text-xs text-mnema-tertiary">{{ $t('automod.exemptHint') }}</p>
      </fieldset>

      <div class="flex items-center gap-3 text-sm text-mnema-text">
        <button id="am-enabled" type="button" role="switch" class="switch" :aria-checked="form.enabled ? 'true' : 'false'" aria-labelledby="am-enabled-label" data-testid="am-enabled" @click="form.enabled = !form.enabled"></button>
        <span id="am-enabled-label">{{ $t('automod.enabled') }}</span>
      </div>

      <p v-if="problem" class="text-xs text-mnema-danger" role="alert" data-testid="am-problem">{{ problem }}</p>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-ghost" @click="emit('close')">{{ $t('common.cancel') }}</button>
        <button type="submit" :disabled="!!problem || busy" class="btn btn-primary" data-testid="am-save">{{ $t('common.save') }}</button>
      </div>
    </form>
  </BaseDialog>
</template>
