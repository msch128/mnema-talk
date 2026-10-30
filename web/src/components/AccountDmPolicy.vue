<script setup lang="ts">
// Who may start a direct message with this member: everyone (the default) or
// nobody. It only decides who can start one; conversations that exist go on,
// and the member can still start their own.
import { onMounted, ref } from 'vue'
import { api } from '../lib/api'
import { decodeChatPrivacy, type ChatPrivacy } from '../types/rest'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import RadioCard from './RadioCard.vue'

const toasts = useToastStore()
const emit = defineEmits<{ saved: [] }>()
const policy = ref<ChatPrivacy['dm_policy']>('everyone')
const loaded = ref(false)
const failed = ref(false)
const busy = ref(false)

async function load() {
  try {
    policy.value = (await api('/api/users/me/privacy', { decode: decodeChatPrivacy })).dm_policy
    loaded.value = true
    failed.value = false
  } catch (error) {
    console.warn('Failed to read the privacy settings:', error)
    failed.value = true
  }
}

async function choose(next: ChatPrivacy['dm_policy']) {
  if (busy.value || next === policy.value) return
  const before = policy.value
  policy.value = next
  busy.value = true
  try {
    policy.value = (await api('/api/users/me/privacy', { method: 'PUT', json: { dm_policy: next }, decode: decodeChatPrivacy })).dm_policy
    emit('saved')
  } catch {
    policy.value = before
    toasts.error(t('dm.policy.failed'))
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="failed && !loaded" class="space-y-2" data-testid="dm-policy-failed">
    <p role="alert" class="text-sm text-mnema-danger">{{ $t('dm.policy.loadFailed') }}</p>
    <button type="button" class="btn btn-ghost btn-sm" data-testid="dm-policy-retry" @click="load">{{ $t('common.retry') }}</button>
  </div>
  <div v-else-if="loaded" class="space-y-3" data-testid="dm-policy">
    <div>
      <h4 id="dm-policy-title" class="settings-subtitle">{{ $t('dm.policy.title') }}</h4>
      <p class="settings-help">{{ $t('dm.policy.intro') }}</p>
    </div>
    <div role="radiogroup" aria-labelledby="dm-policy-title" class="space-y-2">
      <RadioCard
        v-for="option in (['everyone', 'nobody'] as const)"
        :key="option"
        name="dm-policy"
        :value="option"
        :selected="policy === option"
        :disabled="busy"
        :title="$t(`dm.policy.${option}`)"
        :description="$t(`dm.policy.${option}Hint`)"
        :data-dm-policy="option"
        @select="choose(option)"
      />
    </div>
  </div>
</template>
