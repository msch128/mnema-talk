<script setup>
import { computed } from 'vue'
import { Reply, Image as ImageIcon } from '@lucide/vue'
import UserAvatar from './UserAvatar.vue'
import { previewText } from '../lib/replies'

// Discord's "replied to" line above a message: curved connector into the
// author's avatar, 16px avatar, name and a one-line snippet of the original.
const props = defineProps({
  reply: { type: Object, required: true },
  // Tailwind classes positioning the connector relative to this line
  // (it starts at the avatar's horizontal center).
  spineClass: { type: String, default: 'left-[-37px] w-[33px]' }
})

defineEmits(['jump'])

const text = computed(() => previewText(props.reply?.content))
const name = computed(() => props.reply?.display_name || props.reply?.username || 'Unbekannt')
</script>

<template>
  <div class="relative flex items-center gap-1 h-[18px] mb-0.5 min-w-0 text-sm leading-[18px] select-none" data-reply-preview>
    <span
      :class="['reply-spine absolute top-1/2 h-[13px] border-l-2 border-t-2 border-mnema-border-strong rounded-tl-md pointer-events-none', spineClass]"
      aria-hidden="true"
    ></span>

    <template v-if="reply.deleted">
      <span class="w-4 h-4 rounded-full bg-mnema-surface border border-mnema-border flex items-center justify-center flex-shrink-0">
        <Reply class="w-2.5 h-2.5 text-mnema-tertiary" />
      </span>
      <button
        type="button"
        class="italic text-mnema-tertiary truncate min-w-0 text-left hover:text-mnema-muted transition-colors"
        @click.stop="$emit('jump')"
      >
        Ursprüngliche Nachricht wurde gelöscht
      </button>
    </template>

    <template v-else>
      <UserAvatar :user="reply" size="xxs" />
      <span class="font-semibold text-mnema-muted flex-shrink-0 max-w-[40%] truncate">{{ name }}</span>
      <button
        type="button"
        class="reply-preview-jump flex items-center gap-1 min-w-0 text-left text-mnema-tertiary hover:text-mnema-text transition-colors cursor-pointer"
        :title="text || undefined"
        @click.stop="$emit('jump')"
      >
        <span v-if="text" class="truncate">{{ text }}</span>
        <template v-else-if="reply.has_attachments">
          <span class="italic truncate">Klicke, um den Anhang zu sehen</span>
          <ImageIcon class="w-4 h-4 flex-shrink-0" />
        </template>
        <span v-else class="italic truncate">Nachricht</span>
      </button>
    </template>
  </div>
</template>
