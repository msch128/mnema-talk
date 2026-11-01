<script setup lang="ts">
// "Test my connection": a browser-side check of the way to the server's voice port, directly and through
// the relay. No microphone, no Talk. Shown in Admin > Status and under Settings > Audio.
import { computed, onUnmounted, ref } from 'vue'
import { CheckCircle2, CircleHelp, Loader2, Network, TriangleAlert, XCircle } from '@lucide/vue'
import { runConnectionTest } from '../lib/connectionTest'
import { browserTestDeps } from '../lib/connectionTestApi'
import { screenViewersFor, verdictOf, type ConnectionReport, type PathOutcome } from '../lib/connectionVerdict'
import { SOURCE_URL } from '../lib/thirdParty'
import { locale, t } from '../i18n'

type Step = 'candidates' | 'direct' | 'relayUdp' | 'relayTcp' | 'speed'

const { heading = true } = defineProps<{ heading?: boolean }>()
const running = ref(false)
const step = ref<Step>('candidates')
const report = ref<ConnectionReport | null>(null)
const failed = ref(false)
let alive = true
onUnmounted(() => { alive = false })

const verdict = computed(() => (report.value ? verdictOf(report.value) : null))
const paths = computed(() => (report.value
  ? ([['direct', report.value.direct], ['relayUdp', report.value.relayUdp], ['relayTcp', report.value.relayTcp]] as const)
  : []))
const verdictTone = computed(() => ({ direct: 'text-mnema-success', relay_only: 'text-mnema-warning', none: 'text-mnema-danger', inconclusive: 'text-mnema-muted' }[verdict.value?.kind ?? 'inconclusive']))
// t() answers with the key itself when a cause has no fix text.
const fixText = computed(() => {
  const key = `connectionTest.cause.${verdict.value?.cause}.fix`
  const text = t(key)
  return text === key ? '' : text
})
// The upload hint: what the download from the server says about its upload, or why it says nothing.
const speedText = computed(() => {
  const speed = report.value?.speed
  if (!speed) return ''
  if (speed.outcome !== 'ok') return t(`connectionTest.speed.${speed.outcome}`)
  const mbps = new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(speed.mbps)
  return t('connectionTest.speed.ok', { mbps, count: screenViewersFor(speed.mbps) })
})
const docsUrl = `${SOURCE_URL}/blob/main/doc/troubleshooting-voice.md`
const outcomeLabel = (o: PathOutcome) => `connectionTest.outcome.${o}`

async function run() {
  running.value = true
  failed.value = false
  report.value = null
  try {
    const result = await runConnectionTest(browserTestDeps, s => { if (alive) step.value = s })
    if (alive) report.value = result
  } catch {
    if (alive) failed.value = true
  } finally {
    if (alive) running.value = false
  }
}
</script>

<template>
  <div class="space-y-3" data-testid="connection-test">
    <div v-if="heading" class="flex items-center gap-2">
      <Network class="w-4 h-4 text-mnema-accent shrink-0" aria-hidden="true" />
      <h4 class="text-xs uppercase font-semibold font-mono text-mnema-tertiary tracking-wider">{{ $t('connectionTest.title') }}</h4>
    </div>
    <p class="text-xs text-mnema-muted">{{ $t('connectionTest.intro') }}</p>
    <div class="flex flex-wrap items-center gap-3">
      <button type="button" class="btn btn-sm" :disabled="running" data-testid="connection-test-run" @click="run">
        <Loader2 v-if="running" class="w-4 h-4 animate-spin" aria-hidden="true" />
        {{ running ? $t('connectionTest.running') : report || failed ? $t('connectionTest.again') : $t('connectionTest.run') }}
      </button>
      <p v-if="running" class="text-xs text-mnema-muted" role="status" data-testid="connection-test-step">{{ $t(`connectionTest.step.${step}`) }}</p>
    </div>

    <p v-if="failed" class="text-xs text-mnema-danger" role="alert" data-testid="connection-test-failed">{{ $t('connectionTest.failed') }}</p>

    <div v-if="report && verdict" class="space-y-3" role="status" data-testid="connection-test-result">
      <p class="flex items-start gap-2 text-sm font-semibold" :class="verdictTone" :data-kind="verdict.kind" data-testid="connection-test-verdict">
        <CheckCircle2 v-if="verdict.kind === 'direct'" class="w-4 h-4 mt-0.5 shrink-0" aria-hidden="true" />
        <TriangleAlert v-else-if="verdict.kind === 'relay_only'" class="w-4 h-4 mt-0.5 shrink-0" aria-hidden="true" />
        <XCircle v-else-if="verdict.kind === 'none'" class="w-4 h-4 mt-0.5 shrink-0" aria-hidden="true" />
        <CircleHelp v-else class="w-4 h-4 mt-0.5 shrink-0" aria-hidden="true" />
        <span>{{ $t(`connectionTest.verdict.${verdict.kind}`) }}</span>
      </p>
      <p class="text-xs text-mnema-text" :data-cause="verdict.cause" data-testid="connection-test-cause">{{ $t(`connectionTest.cause.${verdict.cause}.text`) }}</p>
      <p v-if="fixText" class="text-xs text-mnema-text" data-testid="connection-test-fix">{{ fixText }}</p>
      <ul class="text-xs text-mnema-muted space-y-1">
        <li v-for="[name, path] in paths" :key="name" :data-testid="`connection-test-path-${name}`" :data-outcome="path.outcome">
          {{ $t(`connectionTest.path.${name}`) }}: {{ $t(outcomeLabel(path.outcome)) }}
        </li>
        <li v-if="speedText" data-testid="connection-test-speed" :data-outcome="report.speed?.outcome">{{ speedText }}</li>
        <li data-testid="connection-test-addresses">{{ $t('connectionTest.addresses', report.candidates) }}</li>
      </ul>
      <a :href="docsUrl" target="_blank" rel="noopener noreferrer" class="text-xs text-mnema-accent hover:underline">{{ $t('connectionTest.docs') }}</a>
    </div>
  </div>
</template>
