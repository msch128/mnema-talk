<script setup lang="ts">
// Roles of one bot. A bot does not get @everyone: with no role it can do
// nothing. The server decides who may hand out powerful roles; its message is
// shown when it refuses.
import { ref, computed, watch, onMounted } from 'vue'
import { Shield, TriangleAlert, Lock } from '@lucide/vue'
import { api, caughtErrorMessage } from '../lib/api'
import { decodeBotBot } from '../types/rest'
import type { BotBot } from '../types/rest'
import { isPowerfulRole } from '../lib/bots'
import { roleColorCss } from '../lib/permissions'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

const props = defineProps({
  bot: { type: Object as () => BotBot, required: true }
})
const emit = defineEmits<{ updated: [bot: BotBot] }>()

const permissions = usePermissionsStore()
const toasts = useToastStore()
const selected = ref<string[]>([...props.bot.role_ids])
const busy = ref(false)

// Another bot's own role is not offered; this bot's own role is shown ticked.
const roles = computed(() => permissions.roles.filter(r => !r.is_everyone && (!r.bot_user_id || r.bot_user_id === props.bot.user_id)))
// Every bot holds the managed Bot role; it is shown ticked and cannot be changed.
const held = (role: { id: string; managed?: boolean }) => role.managed === true || selected.value.includes(role.id)
const dirty = computed(() => {
  const saved = [...props.bot.role_ids].sort()
  const now = [...selected.value].sort()
  return saved.length !== now.length || saved.some((id, i) => id !== now[i])
})
const warnsPowerful = computed(() => roles.value.some(r => selected.value.includes(r.id) && isPowerfulRole(r.permissions)))

watch(() => props.bot.role_ids, ids => { selected.value = [...ids] })

function toggle(id: string, on: boolean) {
  selected.value = on ? [...selected.value, id] : selected.value.filter(x => x !== id)
}

async function save() {
  if (busy.value || !dirty.value) return
  busy.value = true
  try {
    const bot = await api(`/api/admin/bots/${props.bot.id}/roles`, { method: 'PUT', json: { role_ids: selected.value }, decode: decodeBotBot })
    emit('updated', bot)
    toasts.success(t('bots.roles.saved'))
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('admin.unknownError')))
  } finally {
    busy.value = false
  }
}

onMounted(() => { if (!permissions.roles.length) void permissions.loadRoles() })
</script>

<template>
  <section class="space-y-3" data-testid="bot-roles">
    <h4 class="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary"><Shield class="h-3.5 w-3.5" aria-hidden="true" />{{ $t('bots.roles.title') }}</h4>
    <p class="text-sm text-mnema-tertiary">{{ $t('bots.roles.hint') }}</p>
    <p v-if="!roles.length" class="text-sm text-mnema-tertiary">{{ $t('bots.roles.empty') }}</p>
    <ul v-else class="divide-y divide-mnema-hairline overflow-hidden rounded-lg border border-mnema-hairline bg-mnema-surface">
      <li v-for="role in roles" :key="role.id" class="space-y-1 px-3 py-2" :data-role="role.name">
        <label class="flex items-center gap-2 text-sm text-mnema-text">
          <input type="checkbox" class="accent-mnema-accent" :checked="held(role)" :disabled="role.managed" @change="toggle(role.id, ($event.target as HTMLInputElement).checked)" />
          <span class="h-3 w-3 shrink-0 rounded-full border border-mnema-border-strong" :style="{ background: roleColorCss(role.color) || 'transparent' }"></span>
          <span class="truncate">{{ role.name }}</span>
          <Lock v-if="role.managed" class="h-3 w-3 shrink-0 text-mnema-tertiary" role="img" :aria-label="$t(role.bot_user_id ? 'roles.botRoleHint' : 'roles.managedHint')" data-testid="bot-role-managed" />
        </label>
        <p v-if="role.managed" class="pl-6 text-xs text-mnema-tertiary">{{ $t(role.bot_user_id ? 'bots.roles.managedOwn' : 'bots.roles.managed') }}</p>
        <p v-if="isPowerfulRole(role.permissions)" class="flex items-start gap-1.5 pl-6 text-xs text-mnema-danger" data-testid="bot-role-power">
          <TriangleAlert class="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
          <span>{{ $t('bots.roles.power') }}</span>
        </p>
      </li>
    </ul>
    <p v-if="warnsPowerful" class="rounded-md border border-mnema-danger/40 bg-mnema-danger/10 px-3 py-2 text-sm text-mnema-text" role="alert" data-testid="bot-power-selected">{{ $t('bots.roles.powerSelected') }}</p>
    <button type="button" :disabled="!dirty || busy" class="rounded-md bg-mnema-accent px-3.5 py-1.5 text-sm font-semibold text-mnema-accent-ink shadow-xs transition hover:bg-mnema-accent-hover disabled:opacity-40" data-testid="bot-roles-save" @click="save">{{ $t('bots.roles.save') }}</button>
  </section>
</template>
