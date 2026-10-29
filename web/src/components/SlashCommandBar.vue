<script setup lang="ts">
import type { PropType } from 'vue'
import { computed, nextTick, onMounted, ref } from 'vue'
import { Play, X } from '@lucide/vue'
import type { BotChannelCommand } from '../types/rest'
import type { OptionValues } from '../lib/slashCommands'
import { OPTION, choiceLabel, commandDescription, optionDescription } from '../lib/slashCommands'
import { useChatStore } from '../stores/chat'

// The picked slash command above the composer: its options as fields, a Run
// button, and Esc to drop it. Values live in the composable (useSlashCommands).
defineProps({
  command: { type: Object as PropType<BotChannelCommand>, required: true },
  values: { type: Object as PropType<OptionValues>, required: true },
  running: { type: Boolean, default: false }
})
const emit = defineEmits<{ run: []; cancel: []; set: [name: string, value: string | boolean] }>()

const chat = useChatStore()
const root = ref<HTMLElement | null>(null)
const members = computed(() => chat.members.filter(m => m.id))
const channels = computed(() => chat.allChannels.filter(c => c.type === 'text'))

onMounted(async () => {
  await nextTick()
  root.value?.querySelector<HTMLElement>('input, select')?.focus()
})

function set(name: string, value: string | boolean) {
  emit('set', name, value)
}
// Enter in a checkbox toggles nothing and must not run the command; IME Enter only commits a candidate.
const isTextField = (el: HTMLElement) => el.tagName === 'INPUT' && (el as HTMLInputElement).type !== 'checkbox'
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('cancel')
  } else if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && e.keyCode !== 229 && isTextField(e.target as HTMLElement)) {
    e.preventDefault()
    emit('run')
  }
}
</script>

<template>
  <div
    ref="root"
    data-testid="slash-bar"
    role="group"
    :aria-label="$t('slash.barLabel', { name: command.name })"
    class="mb-1.5 rounded-lg border border-mnema-border bg-mnema-raised px-3 py-2 text-sm"
    @keydown="onKey"
  >
    <div class="flex items-center gap-2">
      <span class="font-mono font-semibold text-mnema-text">/{{ command.name }}</span>
      <span class="min-w-0 flex-1 truncate text-xs text-mnema-tertiary">{{ commandDescription(command) }} · {{ command.builtin ? $t('slash.builtinLabel') : command.bot_name }}</span>
      <button
        type="button"
        class="btn btn-primary btn-sm flex h-7 items-center gap-1"
        :disabled="running"
        data-testid="slash-run"
        @click="emit('run')"
      >
        <Play class="h-3.5 w-3.5" />
        {{ $t('slash.run') }}
      </button>
      <button
        type="button"
        class="flex h-7 w-7 items-center justify-center rounded-full text-mnema-tertiary transition hover:bg-mnema-surface hover:text-mnema-text"
        :aria-label="$t('slash.cancel')"
        data-testid="slash-cancel"
        @click="emit('cancel')"
      >
        <X class="h-4 w-4" />
      </button>
    </div>
    <div v-if="command.options.length" class="mt-2 grid gap-2 sm:grid-cols-2">
      <label v-for="o in command.options" :key="o.name" class="flex min-w-0 flex-col gap-0.5 text-xs text-mnema-muted">
        <span>
          <span class="font-semibold text-mnema-text">{{ o.name }}</span>
          <span v-if="o.required" class="text-mnema-accent" :title="$t('slash.required')"> *</span>
          · {{ optionDescription(command, o) }}
        </span>
        <input
          v-if="o.type === OPTION.boolean"
          type="checkbox"
          class="h-4 w-4 self-start"
          :checked="values[o.name] === true"
          :data-option="o.name"
          @change="set(o.name, ($event.target as HTMLInputElement).checked)"
        />
        <select
          v-else-if="o.choices?.length"
          class="h-8 rounded-md border border-mnema-border bg-mnema-elevated px-2 text-sm text-mnema-text"
          :value="values[o.name]"
          :data-option="o.name"
          @change="set(o.name, ($event.target as HTMLSelectElement).value)"
        >
          <option v-if="!o.required" value="" />
          <option v-for="c in o.choices" :key="c" :value="c">{{ choiceLabel(command, o, c) }}</option>
        </select>
        <select
          v-else-if="o.type === OPTION.user"
          class="h-8 rounded-md border border-mnema-border bg-mnema-elevated px-2 text-sm text-mnema-text"
          :value="values[o.name]"
          :data-option="o.name"
          @change="set(o.name, ($event.target as HTMLSelectElement).value)"
        >
          <option value="" />
          <option v-for="m in members" :key="m.id" :value="m.id">{{ m.display_name || m.username }}</option>
        </select>
        <select
          v-else-if="o.type === OPTION.channel"
          class="h-8 rounded-md border border-mnema-border bg-mnema-elevated px-2 text-sm text-mnema-text"
          :value="values[o.name]"
          :data-option="o.name"
          @change="set(o.name, ($event.target as HTMLSelectElement).value)"
        >
          <option value="" />
          <option v-for="c in channels" :key="c.id" :value="c.id"># {{ c.name }}</option>
        </select>
        <input
          v-else
          :type="o.type === OPTION.integer ? 'number' : 'text'"
          :inputmode="o.type === OPTION.integer ? 'numeric' : undefined"
          class="h-8 rounded-md border border-mnema-border bg-mnema-elevated px-2 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"
          :value="values[o.name]"
          :data-option="o.name"
          @input="set(o.name, ($event.target as HTMLInputElement).value)"
        />
      </label>
    </div>
  </div>
</template>
