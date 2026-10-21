// The welcome flow's state: which steps run, where the member is, and what
// finishing does. The first run shows every step; later only the rules, and
// only while they are pending. Rules cannot be skipped or closed.
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { navigate } from '../lib/router'
import { welcomeSteps, isFirstRun, type WelcomeStep } from '../lib/welcomeSteps'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useOnboardingStore } from '../stores/onboarding'

function useOnReturn(callback: () => void) {
  const onVisible = () => { if (document.visibilityState === 'visible') callback() }
  onMounted(() => document.addEventListener('visibilitychange', onVisible))
  onBeforeUnmount(() => document.removeEventListener('visibilitychange', onVisible))
}

export function useWelcomeFlow() {
  const auth = useAuthStore()
  const chat = useChatStore()
  const onboarding = useOnboardingStore()

  const steps = ref<WelcomeStep[]>([])
  const index = ref(0)
  const firstRun = ref(false)
  const accepting = ref(false)

  const step = computed<WelcomeStep | null>(() => steps.value[index.value] ?? null)
  const open = computed(() => step.value !== null)
  const stepNumber = computed(() => index.value + 1)
  const canClose = computed(() => step.value !== 'rules')

  /** Decides whether to open; existing accounts are marked done instead. */
  async function evaluate() {
    await onboarding.ensureLoaded()
    const state = onboarding.state
    const user = auth.user
    if (!state || !user || open.value) return
    firstRun.value = isFirstRun(state, user.created_at)
    if (!state.welcome_done && !firstRun.value) void onboarding.save({ welcome_done: true }).catch(() => undefined)
    index.value = 0
    steps.value = welcomeSteps(state, { firstRun: firstRun.value, canAskNotifications: chat.notificationPermission === 'default' })
    onboarding.welcomeOpen = open.value
  }

  function openStartChannel() {
    const id = onboarding.state?.start_channel_id
    if (id && chat.allChannels.some(c => c.id === id)) navigate(`/c/${id}`)
  }

  function finish() {
    const wasFirstRun = firstRun.value
    steps.value = []
    firstRun.value = false
    onboarding.welcomeOpen = false
    if (!wasFirstRun) return
    void onboarding.save({ welcome_done: true }).catch(() => undefined)
    openStartChannel()
  }

  function next() {
    if (index.value + 1 < steps.value.length) index.value += 1
    else finish()
  }

  /** Skipping or closing: the rules still come first when they are pending. */
  function close() {
    if (!canClose.value) return
    const rules = steps.value.indexOf('rules')
    if (rules > index.value) index.value = rules
    else finish()
  }

  async function accept() {
    const version = onboarding.state?.rules_version ?? 0
    accepting.value = true
    try {
      if (await onboarding.acceptRules(version)) next()
    } finally {
      accepting.value = false
    }
  }

  onMounted(() => { void evaluate() })
  // A rules version raised while the app was in the background shows up on return.
  useOnReturn(() => { if (!open.value) void onboarding.reload().then(evaluate) })
  onBeforeUnmount(() => { onboarding.welcomeOpen = false })

  return { step, stepNumber, total: computed(() => steps.value.length), open, canClose, accepting, next, close, accept }
}
