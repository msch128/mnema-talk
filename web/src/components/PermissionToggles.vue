<script setup lang="ts">
// The permissions of a role (on/off switches) or of one overwrite
// (deny / inherit / allow per permission), each with a short explanation.
import type { PropType } from 'vue'
import { PERMISSION_BITS } from '../lib/permissions'
import type { PermissionKey } from '../lib/permissions'

const props = defineProps({
  keys: { type: Array as PropType<readonly PermissionKey[]>, required: true },
  // 'switch': allow bits only. 'tri': allow and deny bits.
  mode: { type: String as PropType<'switch' | 'tri'>, default: 'switch' },
  allow: { type: Number, default: 0 },
  deny: { type: Number, default: 0 },
  // Bits the editor may not change (permissions they do not hold themselves).
  locked: { type: Number, default: 0 },
  // The whole list is read-only.
  readonly: { type: Boolean, default: false }
})
const emit = defineEmits<{ 'update:allow': [value: number]; 'update:deny': [value: number] }>()

function bit(key: PermissionKey) { return PERMISSION_BITS[key] }
function isLocked(key: PermissionKey) { return props.readonly || (props.locked & bit(key)) !== 0 }

function setSwitch(key: PermissionKey, on: boolean) {
  emit('update:allow', on ? props.allow | bit(key) : props.allow & ~bit(key))
}

type State = 'deny' | 'inherit' | 'allow'
function stateOf(key: PermissionKey): State {
  if (props.allow & bit(key)) return 'allow'
  if (props.deny & bit(key)) return 'deny'
  return 'inherit'
}
function setState(key: PermissionKey, state: State) {
  emit('update:allow', state === 'allow' ? props.allow | bit(key) : props.allow & ~bit(key))
  emit('update:deny', state === 'deny' ? props.deny | bit(key) : props.deny & ~bit(key))
}
const STATES: readonly State[] = ['deny', 'inherit', 'allow']
</script>

<template>
  <ul class="divide-y divide-mnema-hairline rounded-lg border border-mnema-hairline bg-mnema-surface">
    <li v-for="key in keys" :key="key" class="flex items-center justify-between gap-4 px-4 py-2.5" :data-permission="key">
      <div class="min-w-0">
        <div class="text-sm font-medium text-mnema-text">{{ $t(`perm.${key}.name`) }}</div>
        <div class="text-xs text-mnema-tertiary">{{ $t(`perm.${key}.desc`) }}</div>
      </div>

      <button
        v-if="mode === 'switch'"
        type="button"
        role="switch"
        :aria-checked="(allow & bit(key)) !== 0"
        :aria-label="$t(`perm.${key}.name`)"
        :disabled="isLocked(key)"
        class="switch"
        @click="setSwitch(key, (allow & bit(key)) === 0)"
      >
      </button>

      <div v-else role="group" :aria-label="$t(`perm.${key}.name`)" class="flex shrink-0 overflow-hidden rounded-md border border-mnema-border-strong text-xs font-medium">
        <button
          v-for="state in STATES"
          :key="state"
          type="button"
          :aria-pressed="stateOf(key) === state"
          :disabled="isLocked(key)"
          :data-state="state"
          :class="[
            'px-2.5 py-1 transition focus:outline-hidden focus-visible:ring-2 focus-visible:ring-mnema-accent disabled:opacity-40',
            stateOf(key) === state
              ? (state === 'allow' ? 'bg-mnema-accent text-mnema-accent-ink' : state === 'deny' ? 'bg-mnema-danger text-mnema-accent-ink' : 'bg-mnema-hover text-mnema-text')
              : 'bg-mnema-elevated text-mnema-tertiary hover:text-mnema-text'
          ]"
          @click="setState(key, state)"
        >{{ $t(`perm.${state}`) }}</button>
      </div>
    </li>
  </ul>
</template>
