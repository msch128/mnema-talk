// The poll of one message as its card needs it: the tallies from the message
// (kept current by poll_update), the member's own votes, a clock that flips the
// card to "ended" on time, and voting/ending.
import type { Ref } from 'vue'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Message, Poll } from '../types/domain'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { caughtErrorMessage } from '../lib/api'
import { isEnded, nextSelection, remaining, votedFor } from '../lib/polls'
import { t } from '../i18n'
import { visibleInterval } from '../lib/pageVisibility'

const TICK_MS = 15_000
// setTimeout cannot wait longer than a signed 32-bit number of milliseconds.
const MAX_TIMEOUT_MS = 2 ** 31 - 1

/** Keeps `now` current: a slow tick for the counter, and a timeout that flips the card the moment voting ends. */
function useClock(now: Ref<number>, closesAt: () => string) {
  let stopTimer: (() => void) | undefined
  let flip: ReturnType<typeof setTimeout> | undefined
  function scheduleFlip() {
    clearTimeout(flip)
    const wait = Date.parse(closesAt()) - Date.now()
    if (wait > 0 && wait < MAX_TIMEOUT_MS) flip = setTimeout(() => { now.value = Date.now() }, wait + 50)
  }
  onMounted(() => {
    stopTimer = visibleInterval(() => { now.value = Date.now() }, TICK_MS)
    scheduleFlip()
  })
  watch(closesAt, scheduleFlip)
  onBeforeUnmount(() => {
    stopTimer?.()
    clearTimeout(flip)
  })
}

export function usePoll(message: () => Message) {
  const chat = useChatStore()
  const auth = useAuthStore()
  const permissions = usePermissionsStore()
  const toasts = useToastStore()

  const now = ref(Date.now())
  const poll = computed<Poll>(() => message().poll as Poll)
  useClock(now, () => poll.value.closes_at)
  const ended = computed(() => isEnded(poll.value, now.value))
  const left = computed(() => remaining(poll.value, now.value))
  const busy = ref(false)
  // An anonymous poll does not store the choice: the member has voted or has not, and the options
  // they tick before voting are only held here until the vote is sent (it cannot be changed after).
  const hasVoted = ref(false)
  const pending = ref<string[]>([])
  const mine = computed(() => (poll.value.anonymous ? pending.value : votedFor(poll.value, auth.user?.id)))

  const channelId = () => message().channel_id
  const mayVote = computed(() => permissions.canIn(channelId(), 'add_reactions'))
  const canVote = computed(() => !ended.value && mayVote.value && !(poll.value.anonymous && hasVoted.value))
  // An open poll the member may not vote in (voting needs Add reactions): the card says so.
  const votingBlocked = computed(() => !ended.value && !mayVote.value)
  // The vote of an anonymous poll is final: it is sent with a button, after ticking.
  const submitsLater = computed(() => poll.value.anonymous === true)
  const canEnd = computed(() => !ended.value && (message().user_id === auth.user?.id || permissions.canIn(channelId(), 'manage_messages')))

  async function loadMine() {
    if (!poll.value.anonymous) return
    try {
      hasVoted.value = (await chat.fetchPoll(channelId(), message().id)).has_voted === true
    } catch (err) {
      void err // The card still shows the tallies; a vote answers with the member's choice.
    }
  }
  watch(() => message().id, loadMine, { immediate: true })
  // An anonymous poll pushes no running tally; when it ends, fetch the result.
  watch(ended, done => { if (done && poll.value.anonymous) void loadMine() })

  async function run(action: () => Promise<{ has_voted?: boolean }>) {
    if (busy.value) return
    busy.value = true
    try {
      hasVoted.value = (await action()).has_voted === true
      pending.value = []
    } catch (err) {
      toasts.error(caughtErrorMessage(err, t('poll.failed')))
    } finally {
      busy.value = false
    }
  }

  // A poll that names its voters takes a click as the vote; an anonymous one only ticks the option.
  function pick(optionId: string) {
    if (!canVote.value) return Promise.resolve()
    if (submitsLater.value) {
      pending.value = nextSelection(pending.value, optionId, poll.value.multiple)
      return Promise.resolve()
    }
    return run(() => chat.votePoll(channelId(), message().id, nextSelection(mine.value, optionId, poll.value.multiple)))
  }
  const submit = () => canVote.value && submitsLater.value && pending.value.length > 0
    ? run(() => chat.votePoll(channelId(), message().id, pending.value))
    : Promise.resolve()
  const end = () => canEnd.value ? run(() => chat.endPoll(channelId(), message().id)) : Promise.resolve()

  return { poll, ended, left, mine, busy, canVote, votingBlocked, canEnd, hasVoted, submitsLater, pick, submit, end }
}
