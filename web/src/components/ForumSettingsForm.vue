<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Loader2 } from '@lucide/vue'
import { charCount } from '../lib/textLength'
import { caughtErrorMessage } from '../lib/api'
import { updateForumSettings } from '../lib/forumApi'
import type { ForumSettings } from '../lib/forumApi'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'
import SwitchRow from './SwitchRow.vue'

// The forum's rules: whether a post needs a tag, and the guidelines shown above
// the new-post form.
const props = defineProps<{
  channelId: string
  settings: ForumSettings
  /** The forum has no tags to require. */
  noTags: boolean
}>()

const emit = defineEmits<{ saved: [settings: ForumSettings] }>()

const MAX_GUIDELINES = 1000
const toasts = useToastStore()
const requireTag = ref(props.settings.require_tag)
const guidelines = ref(props.settings.guidelines)
const saving = ref(false)

watch(() => props.settings, s => {
  requireTag.value = s.require_tag
  guidelines.value = s.guidelines
})

const dirty = computed(() => requireTag.value !== props.settings.require_tag || guidelines.value.trim() !== props.settings.guidelines)

async function save() {
  if (!dirty.value || saving.value) return
  saving.value = true
  try {
    emit('saved', await updateForumSettings(props.channelId, { require_tag: requireTag.value, guidelines: guidelines.value.trim() }))
  } catch (err) {
    toasts.error(caughtErrorMessage(err, t('forum.settingsFailed')))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <section class="space-y-3" data-testid="forum-settings">
    <h3 class="text-xs font-semibold uppercase tracking-wider text-mnema-secondary">{{ $t('forum.rulesTitle') }}</h3>
    <SwitchRow
      v-model="requireTag"
      data-testid="forum-require-tag"
      :title="$t('forum.requireTagTitle')"
      :hint="noTags ? $t('forum.requireTagNoTags') : $t('forum.requireTagHint')"
    />
    <div>
      <div class="mb-1.5 flex items-center justify-between">
        <label for="forum-guidelines" class="text-sm font-semibold text-mnema-text">{{ $t('forum.guidelinesTitle') }}</label>
        <span class="text-xs tabular-nums text-mnema-tertiary">{{ charCount(guidelines) }}/{{ MAX_GUIDELINES }}</span>
      </div>
      <textarea
        id="forum-guidelines"
        v-model="guidelines"
        v-max-chars="MAX_GUIDELINES"
        rows="3"
        :placeholder="$t('forum.guidelinesPlaceholder')"
        class="min-h-[72px] w-full resize-y rounded-lg border border-mnema-border bg-mnema-surface p-3 text-sm text-mnema-text placeholder:text-mnema-tertiary focus:outline-hidden focus:border-mnema-accent"
      ></textarea>
    </div>
    <div class="flex justify-end">
      <button type="button" data-testid="forum-settings-save" :disabled="!dirty || saving" class="btn btn-secondary flex items-center gap-1.5 px-3 py-1.5 text-xs" @click="save">
        <Loader2 v-if="saving" class="h-3.5 w-3.5 animate-spin" />
        <span>{{ $t('common.save') }}</span>
      </button>
    </div>
  </section>
</template>
