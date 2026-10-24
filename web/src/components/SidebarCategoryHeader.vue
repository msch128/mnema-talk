<script setup lang="ts">
import type { PropType } from 'vue'
import type { DropIndicator } from './presentationTypes'
// Header of a sidebar category: collapse toggle, and for admins "+" / delete.
// Admins can drag the header to move the whole category, or move it with
// Alt+ArrowUp / Alt+ArrowDown on the toggle.
import { computed } from 'vue'
import { Plus, Trash2, ChevronDown, ChevronRight, BellOff, AtSign } from '@lucide/vue'
import { useChatStore } from '../stores/chat'

const props = defineProps({
  category: { type: Object as PropType<{ id: string; name: string }>, required: true },
  collapsed: { type: Boolean, default: false },
  admin: { type: Boolean, default: false },
  // This category is being dragged.
  dragging: { type: Boolean, default: false },
  // Drop indicator: 'top' | 'bottom' | 'inside' (a channel goes into it) | ''.
  indicator: { type: String as PropType<DropIndicator>, default: '' },
  flash: { type: Boolean, default: false },
  hintId: { type: String, default: '' },
  // What the category's channels have waiting; shown while it is collapsed.
  unreadChannels: { type: Number, default: 0 },
  mentions: { type: Number, default: 0 }
})

const emit = defineEmits<{ toggle: []; menu: [event: MouseEvent | KeyboardEvent]; 'create-channel': []; delete: []; 'drag-start': [event: PointerEvent]; move: [direction: -1 | 1] }>()

// The category's own setting, when it has one that is still in force.
const chatStore = useChatStore()
const ownLevel = computed(() => chatStore.notifySettings.categories.find(c => c.category_id === props.category.id)?.level)

function moveKey(e: KeyboardEvent, dir: -1 | 1) {
  if (!props.admin) return
  e.preventDefault()
  emit('move', dir)
}
</script>

<template>
  <div
    :class="[
      'relative h-6 max-md:h-11 flex items-center gap-1 pr-1 rounded-sm transition-colors motion-reduce:transition-none',
      indicator === 'inside' && 'bg-mnema-accent/15 ring-1 ring-inset ring-mnema-accent',
      dragging && 'opacity-40',
      flash && 'nav-flash'
    ]"
    data-drop="header"
    :data-id="category.id"
    @contextmenu="emit('menu', $event)"
    @pointerdown="emit('drag-start', $event)"
  >
    <span
      v-if="indicator === 'top' || indicator === 'bottom'"
      :class="['drop-line', indicator === 'top' ? '-top-1' : '-bottom-0.5']"
      data-drop-indicator
      aria-hidden="true"
    ></span>

    <button
      type="button"
      class="flex-1 min-w-0 h-6 max-md:h-11 pl-0.5 flex items-center gap-0.5 text-xs font-semibold uppercase tracking-wide text-mnema-tertiary hover:text-mnema-muted transition-colors focus:outline-hidden focus-visible:text-mnema-text focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-mnema-accent rounded-sm"
      :aria-expanded="collapsed ? 'false' : 'true'"
      aria-haspopup="menu"
      :data-category-toggle="category.id"
      :aria-describedby="admin && hintId ? hintId : undefined"
      :aria-keyshortcuts="admin ? 'Alt+ArrowUp Alt+ArrowDown' : undefined"
      @click="emit('toggle')"
      @keydown.f10.shift.prevent="emit('menu', $event)"
      @keydown.context-menu.prevent="emit('menu', $event)"
      @keydown.alt.up="moveKey($event, -1)"
      @keydown.alt.down="moveKey($event, 1)"
    >
      <ChevronRight v-if="collapsed" class="w-3 h-3 shrink-0" />
      <ChevronDown v-else class="w-3 h-3 shrink-0" />
      <span class="truncate">{{ category.name }}</span>
      <BellOff v-if="ownLevel === 'mute'" class="w-3 h-3 shrink-0 ml-1" data-testid="category-muted" :aria-label="$t('notifications.bellNothing')" />
      <AtSign v-else-if="ownLevel === 'mentions'" class="w-3 h-3 shrink-0 ml-1" data-testid="category-mentions-only" :aria-label="$t('notifications.bellMentions')" />
      <span
        v-if="collapsed && mentions > 0"
        data-testid="category-mentions"
        class="ml-auto px-1.5 py-0.5 rounded-full bg-mnema-danger text-white text-xs font-bold leading-none min-w-[18px] text-center normal-case tracking-normal"
        :aria-label="$t('sidebar.categoryMentions', { n: mentions })"
      >{{ mentions }}</span>
      <span
        v-else-if="collapsed && unreadChannels > 0"
        data-testid="category-unread"
        class="ml-auto w-2 h-2 mr-1 rounded-full bg-mnema-text shrink-0"
        role="img"
        :aria-label="$t('sidebar.categoryUnread')"
      ></span>
    </button>
    <div
      v-if="admin"
      class="max-md:hidden flex items-center gap-0.5 shrink-0 opacity-0 group-hover/cat:opacity-100 focus-within:opacity-100 transition"
    >
      <button
        type="button"
        data-no-drag
        @click.stop="emit('create-channel')"
        v-tooltip="$t('channel.create')"
        :aria-label="$t('channel.create')"
        class="btn btn-icon w-5 h-5"
      >
        <Plus class="w-4 h-4" />
      </button>
      <button
        type="button"
        data-no-drag
        @click.stop="emit('delete')"
        v-tooltip="$t('sidebar.deleteCategory')"
        :aria-label="$t('sidebar.deleteCategory')"
        class="btn btn-icon w-5 h-5 hover:text-mnema-danger"
      >
        <Trash2 class="w-3.5 h-3.5" />
      </button>
    </div>
  </div>
</template>
