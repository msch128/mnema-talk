// The poll of one message as its card needs it: the tallies from the message
// (kept current by poll_update), the member's own votes, a clock that flips the
// card to "ended" on time, and voting/ending.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Message, Poll } from '../types/domain'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { caughtErrorMessage } from '../lib/api'
import { isEnded, nextSelection, remaining, votedFor } from '../lib/polls'
import { t } from '../i18n'

const TICK_MS = 15_000
// setTimeout cannot wait longer than a signed 32-bit number of milliseconds.
const MAX_TIMEOUT_MS = 2 ** 31 - 1

export function usePoll(message: () => Message) {
  const chat = useChatStore()
  const auth = useAuthStore()
  const permissions = usePermissionsStore()
  const toasts = useToastStore()

  const now = ref(Date.now())
  const poll = computed<Poll>(() => message().poll as Poll)
  const ended = computed(() => isEnded(poll.value, now.value))
  const left = computed(() => remaining(poll.value, now.value))
  const busy = ref(false)
  // Only an anonymous poll hides the member's own choice in the tallies.
  const anonymousVotes = ref<string[]>([])
  const mine = computed(() => (poll.value.anonymous ? anonymousVotes.value : votedFor(poll.value, auth.user?.id)))

  const channelId = () => message().channel_id
  const canVote = computed(() => !ended.value && permissions.canIn(channelId(), 'add_reactions'))
  const canEnd = computed(() => !ended.value && (message().user_id === auth.user?.id || permissions.canIn(channelId(), 'manage_messages')))

  // The minute counter ticks; a timeout flips the card the moment voting ends.
  let timer: ReturnType<typeof setInterval> | undefined
  let flip: ReturnType<typeof setTimeout> | undefined
  function scheduleFlip() {
    clearTimeout(flip)
    const wait = Date.parse(poll.value.closes_at) - Date.now()
    if (wait > 0 && wait < MAX_TIMEOUT_MS) flip = setTimeout(() => { now.value = Date.now() }, wait + 50)
  }
  onMounted(() => {
    timer = setInterval(() => { now.value = Date.now() }, TICK_MS)
    scheduleFlip()
  })
  watch(() => poll.value.closes_at, scheduleFlip)
  onBeforeUnmount(() => {
    clearInterval(timer)
    clearTimeout(flip)
  })

  async function loadMine() {
    if (!poll.value.anonymous) return
    try {
      anonymousVotes.value = (await chat.fetchPoll(channelId(), message().id)).my_votes
    } catch (err) {
      void err // The card still shows the tallies; a vote answers with the member's choice.
    }
  }
  watch(() => message().id, loadMine, { immediate: true })

  async function run(action: () => Promise<{ my_votes: string[] }>) {
    if (busy.value) return
    busy.value = true
    try {
      anonymousVotes.value = (await action()).my_votes
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('poll.failed')))
    } finally {
      busy.value = false
    }
  }

  const vote = (optionId: string) => canVote.value
    ? run(() => chat.votePoll(channelId(), message().id, nextSelection(mine.value, optionId, poll.value.multiple)))
    : Promise.resolve()
  const end = () => canEnd.value ? run(() => chat.endPoll(channelId(), message().id)) : Promise.resolve()

  return { poll, ended, left, mine, busy, canVote, canEnd, vote, end }
}
