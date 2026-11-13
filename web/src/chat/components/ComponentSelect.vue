<script setup lang="ts">
// A select menu of a bot message: a button that opens a listbox right below it
// (in the flow of the message, so it is never cut off by the viewport). Arrow
// keys move, Enter or Space picks, Esc closes. A single choice is sent when it
// is picked; several are sent with Confirm once their number fits the menu.
// The button shows the pick while it is on its way; a pick the server refuses
// goes back to the one before (the refusal itself is said under the components).
import type { PropType } from 'vue'
import { computed, nextTick, ref, useId, watch } from 'vue'
import { Check, ChevronDown } from '@lucide/vue'
import { ROLE_SELECT, selectBounds, withinBounds, type Component } from '../lib/components'
import { useSelectOptions } from '../composables/useSelectOptions'
import { usePermissionsStore } from '../../stores/permissions'

const props = defineProps({
  component: { type: Object as PropType<Component>, required: true },
  pending: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  /** Sends a pick; true when the server took it. */
  send: { type: Function as PropType<(values: string[]) => Promise<boolean>>, required: true }
})

const { choices, searchable, defaults, filtered } = useSelectOptions(() => props.component)
const bounds = computed(() => selectBounds(props.component))
const multi = computed(() => bounds.value.max > 1)
const open = ref(false)
const query = ref('')
const active = ref(0)
const selected = ref<string[]>([...defaults.value])
// What was sent last, shown on the closed button.
const chosen = ref<string[]>([...defaults.value])
watch(defaults, values => { selected.value = [...values]; chosen.value = [...values] })

const id = useId()
const listId = `${id}-list`
const optionId = (i: number) => `${id}-opt-${i}`
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const search = ref<HTMLInputElement | null>(null)
const list = ref<HTMLElement | null>(null)

const shown = computed(() => filtered(query.value))
const labelOf = (value: string) => choices.value.find(c => c.value === value)?.label ?? ''
const summary = computed(() => chosen.value.map(labelOf).filter(Boolean).join(', '))
const valid = computed(() => withinBounds(selected.value.length, bounds.value))

async function show() {
  if (props.disabled || props.pending) return
  const roles = usePermissionsStore()
  if (props.component.type === ROLE_SELECT && !roles.roles.length) void roles.loadRoles()
  selected.value = [...chosen.value]
  query.value = ''
  active.value = 0
  open.value = true
  await nextTick()
  ;(search.value ?? list.value)?.focus()
}

function close(focusTrigger = true) {
  open.value = false
  if (focusTrigger) void nextTick(() => trigger.value?.focus())
}

async function submit(values: string[]) {
  const before = chosen.value
  chosen.value = [...values]
  close()
  if (!(await props.send(values))) chosen.value = before
}

function pick(value: string) {
  if (!multi.value) {
    void submit([value])
    return
  }
  if (selected.value.includes(value)) selected.value = selected.value.filter(v => v !== value)
  else if (selected.value.length < bounds.value.max) selected.value = [...selected.value, value]
}

function move(to: number) {
  const count = shown.value.length
  if (!count) return
  active.value = (to + count) % count
  void nextTick(() => document.getElementById(optionId(active.value))?.scrollIntoView({ block: 'nearest' }))
}

const KEYS: Record<string, () => void> = {
  ArrowDown: () => move(active.value + 1),
  ArrowUp: () => move(active.value - 1),
  Home: () => move(0),
  End: () => move(shown.value.length - 1),
  Enter: () => { const c = shown.value[active.value]; if (c) pick(c.value) },
  Escape: () => close()
}

function onKey(event: KeyboardEvent) {
  // In the search field a space is typed; in the list it picks, like Enter.
  const key = event.key === ' ' && event.target !== search.value ? 'Enter' : event.key
  const run = KEYS[key]
  if (!run) return
  event.preventDefault()
  event.stopPropagation()
  run()
}

function onFocusOut(event: FocusEvent) {
  const next = event.relatedTarget
  if (open.value && !(next instanceof Node && root.value?.contains(next))) close(false)
}

watch(query, () => { active.value = 0 })
</script>

<template>
  <div ref="root" class="w-full min-w-0 max-w-sm" data-testid="component-select" @focusout="onFocusOut">
    <button
      ref="trigger"
      type="button"
      class="btn btn-secondary w-full justify-between whitespace-normal text-left"
      aria-haspopup="listbox"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-controls="open ? listId : undefined"
      :aria-busy="pending ? 'true' : undefined"
      :disabled="disabled"
      :title="summary || component.placeholder || undefined"
      @click="open ? close() : show()"
    >
      <span class="min-w-0 flex-1 truncate" :class="summary ? 'text-mnema-text' : 'text-mnema-tertiary'">{{ summary || component.placeholder || $t('components.choose') }}</span>
      <ChevronDown class="h-4 w-4 shrink-0 text-mnema-tertiary transition-transform" :class="open ? 'rotate-180' : ''" aria-hidden="true" />
    </button>

    <div v-if="open" class="mt-1 rounded-lg border border-mnema-border bg-mnema-elevated p-1 shadow-lg" data-testid="component-select-panel" @keydown="onKey">
      <input
        v-if="searchable"
        ref="search"
        v-model="query"
        type="search"
        role="combobox"
        aria-autocomplete="list"
        aria-expanded="true"
        :aria-controls="listId"
        :aria-activedescendant="shown.length ? optionId(active) : undefined"
        :aria-label="$t('components.search')"
        :placeholder="$t('components.search')"
        class="mb-1 w-full rounded-md border border-mnema-border-field bg-mnema-surface px-2.5 py-1.5 text-sm text-mnema-text outline-hidden focus:border-mnema-accent"
        data-testid="component-select-search"
      />
      <ul
        :id="listId"
        ref="list"
        role="listbox"
        :tabindex="searchable ? -1 : 0"
        :aria-label="component.placeholder || $t('components.choose')"
        :aria-multiselectable="multi ? 'true' : undefined"
        :aria-activedescendant="!searchable && shown.length ? optionId(active) : undefined"
        class="focus-ring max-h-60 overflow-y-auto rounded-md outline-hidden"
      >
        <li
          v-for="(choice, i) in shown"
          :id="optionId(i)"
          :key="choice.value"
          role="option"
          :aria-selected="selected.includes(choice.value) ? 'true' : 'false'"
          :data-value="choice.value"
          :class="['flex cursor-pointer items-start gap-2 rounded-md px-2 py-1.5 text-sm', i === active ? 'bg-mnema-hover' : '']"
          @mousemove="active = i"
          @mousedown.prevent
          @click="pick(choice.value)"
        >
          <Check :class="['mt-0.5 h-3.5 w-3.5 shrink-0 text-mnema-accent', selected.includes(choice.value) ? '' : 'invisible']" aria-hidden="true" />
          <span v-if="choice.color" class="mt-1.5 h-2 w-2 shrink-0 rounded-full" :style="{ backgroundColor: choice.color }" aria-hidden="true" />
          <span class="min-w-0 flex-1">
            <span class="block wrap-break-word text-mnema-text"><span v-if="choice.emoji" class="mr-1" aria-hidden="true">{{ choice.emoji }}</span>{{ choice.prefix }}{{ choice.label }}</span>
            <span v-if="choice.description" class="block wrap-break-word text-xs text-mnema-tertiary">{{ choice.description }}</span>
          </span>
        </li>
      </ul>
      <p v-if="!shown.length" class="px-2 py-1.5 text-sm text-mnema-tertiary">{{ $t('components.noMatches') }}</p>
      <div v-if="multi" class="mt-1 flex flex-wrap items-center justify-between gap-2 border-t border-mnema-hairline px-1 pt-1.5">
        <span class="text-xs text-mnema-tertiary" data-testid="component-select-bounds">{{ bounds.min === bounds.max ? $t('components.pickExactly', { count: bounds.max }) : $t('components.pickRange', { min: bounds.min, max: bounds.max }) }}</span>
        <button type="button" class="btn btn-primary btn-sm" :disabled="!valid" data-testid="component-select-confirm" @click="submit(selected)">{{ $t('components.confirm') }}</button>
      </div>
    </div>
  </div>
</template>
