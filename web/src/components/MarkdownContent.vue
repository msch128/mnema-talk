<script setup lang="ts">
import { computed } from 'vue'
import type { PropType } from 'vue'
import { renderMarkdown } from '../lib/markdown'
import { extractPreviewUrls } from '../lib/chatLogic'
import { copyCodeBlock, openConfirmedLink } from '../lib/markdownActions'
import LinkPreviewCard from './LinkPreviewCard.vue'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { roleColorCss } from '../lib/permissions'
import { roleColors as vRoleColors } from '../directives/roleColor'

const props = defineProps({
  content: {
    type: String,
    default: ''
  },
  // The roles the server recorded as mentioned by this message.
  mentionRoles: {
    type: Array as PropType<string[]>,
    default: () => []
  },
  // Link preview cards below the text (off where the text is only an example).
  previews: {
    type: Boolean,
    default: true
  }
})

const chatStore = useChatStore()
const authStore = useAuthStore()
const permissions = usePermissionsStore()

// Only real members (and @all/@here) are highlighted, and clickable.
const knownNames = computed(() => new Set(chatStore.members.map(m => (m.username || '').toLowerCase())))
// Pills for the recorded roles that still exist, from their stored IDs.
const mentionedRoles = computed(() => props.mentionRoles
  .map(id => permissions.roles.find(r => r.id === id))
  .filter(r => r !== undefined)
  .map(r => ({ name: r.name, me: permissions.myRoleIds.includes(r.id), color: roleColorCss(r.color) })))
const parsedHtml = computed(() => renderMarkdown(props.content, {
  known: knownNames.value,
  me: authStore.user?.username || '',
  roles: mentionedRoles.value
}))

function openMention(el: HTMLElement) {
  const name = el.dataset.mention
  if (!name || name === 'all' || name === 'here') return
  const member = chatStore.members.find(m => (m.username || '').toLowerCase() === name)
  if (member) chatStore.openUserProfile(member)
}

// Only links in plain text get a card, never ones inside code or spoilers.
const links = computed(() => (props.previews ? extractPreviewUrls(props.content, 3) : []))

// Spoilers, copy buttons and masked links work via delegation: the CSP forbids inline handlers.
function onClick(event: MouseEvent) {
  const target = event.target instanceof Element ? event.target : null
  const copy = target?.closest('[data-md-copy]')
  if (copy) {
    void copyCodeBlock(copy)
    return
  }
  if (confirmLink(event, target)) return
  const mention = target?.closest<HTMLElement>('.md-mention[data-mention]')
  if (mention) {
    openMention(mention)
    return
  }
  const spoiler = target?.closest('.md-spoiler')
  if (spoiler) spoiler.classList.toggle('revealed')
}

// A link whose text does not name its destination opens only after a confirmation,
// however it is activated (click, modifier click, middle click, keyboard).
function confirmLink(event: MouseEvent, target: Element | null): boolean {
  const anchor = target?.closest<HTMLAnchorElement>('a.md-link[data-confirm]')
  if (!anchor) return false
  event.preventDefault()
  void openConfirmedLink(anchor)
  return true
}

function onAuxClick(event: MouseEvent) {
  if (event.button === 1) confirmLink(event, event.target instanceof Element ? event.target : null)
}

function isSpoilerElement(target: EventTarget | null): target is Element {
  return target instanceof Element && target.classList.contains('md-spoiler')
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Enter' && event.target instanceof HTMLElement && event.target.dataset.mention) {
    event.preventDefault()
    openMention(event.target)
    return
  }
  if ((event.key === 'Enter' || event.key === ' ') && isSpoilerElement(event.target)) {
    event.preventDefault()
    event.target.classList.toggle('revealed')
  }
}
</script>

<template>
  <div>
    <!-- The only intentional v-html: renderMarkdown escapes all input before adding markup. -->
    <!-- eslint-disable vue/no-v-html -->
    <div
      v-role-colors
      class="markdown-body wrap-break-word whitespace-pre-wrap select-text text-message"
      @click="onClick"
      @auxclick="onAuxClick"
      @keydown="onKeydown"
      v-html="parsedHtml"
    ></div>
    <!-- eslint-enable vue/no-v-html -->

    <div v-if="links.length > 0" class="mt-1 space-y-1">
      <LinkPreviewCard v-for="link in links" :key="link" :url="link" />
    </div>
  </div>
</template>

<style src="./markdownBody.css"></style>
