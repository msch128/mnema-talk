<script setup>
import { ref, computed, onMounted } from 'vue'
import { ShieldCheck, Scale, Server, Lock, Cookie, Code, ExternalLink } from '@lucide/vue'
import BaseDialog from './BaseDialog.vue'
import { t } from '../i18n'

const emit = defineEmits(['close'])

const activeTab = ref('all') // 'all', 'privacy', 'terms', 'operator'

// Raw values from the server; fallbacks are translated at render time.
const operatorRaw = ref({ name: '', email: '', country: '', status: '' })
const operator = computed(() => ({
  name: operatorRaw.value.name || t('legal.defaultOperator'),
  email: operatorRaw.value.email || 'admin@example.com',
  country: operatorRaw.value.country || t('legal.defaultCountry'),
  status: operatorRaw.value.status || t('legal.defaultStatus')
}))
const mediaRetentionDays = ref(0) // 0 = no automatic deletion
const sessionExpiryDays = ref(30)
const stunServers = ref([])
const legalVersion = ref('1.3')

const TABS = ['all', 'operator', 'privacy', 'terms']

onMounted(async () => {
  try {
    const res = await fetch('/api/legal')
    if (res.ok) {
      const data = await res.json()
      operatorRaw.value = {
        name: data.operator_name || '',
        email: data.operator_email || '',
        country: data.operator_country || '',
        status: data.project_notice || ''
      }
      mediaRetentionDays.value = data.media_retention_days || 0
      if (data.session_expiry_days) sessionExpiryDays.value = data.session_expiry_days
      stunServers.value = data.stun_servers || []
      if (data.legal_version) legalVersion.value = data.legal_version
    }
  } catch {
    // Keep generic defaults
  }
})

const OSS_LIBS = [
  { name: 'Go (Golang)', license: 'BSD-3-Clause', url: 'https://go.dev' },
  { name: 'Pion WebRTC', license: 'MIT', url: 'https://pion.ly' },
  { name: 'Vue 3', license: 'MIT', url: 'https://vuejs.org' },
  { name: 'Tailwind CSS', license: 'MIT', url: 'https://tailwindcss.com' },
  { name: 'Lucide Icons', license: 'ISC', url: 'https://lucide.dev' },
  { name: 'SeaweedFS', license: 'Apache-2.0', url: 'https://github.com/seaweedfs/seaweedfs' },
  { name: 'PostgreSQL 17', license: 'PostgreSQL License', url: 'https://www.postgresql.org' }
]
</script>

<template>
  <BaseDialog
    :title="$t('legal.title')"
    :subtitle="$t('legal.subtitle')"
    panel-class="max-w-3xl max-h-[90vh] text-mnema-text"
    @close="emit('close')"
  >
    <template #badge>
      <span class="flex-shrink-0 rounded border border-mnema-border bg-mnema-surface px-1.5 py-0.5 font-mono text-xs font-normal text-mnema-tertiary">{{ $t('legal.version', { version: legalVersion }) }}</span>
    </template>

    <!-- Navigation tabs -->
    <div class="flex items-center gap-1 px-6 py-2 border-b border-mnema-hairline bg-mnema-canvas/40 text-sm overflow-x-auto flex-shrink-0">
      <button
        v-for="tab in TABS"
        :key="tab"
        type="button"
        :aria-pressed="activeTab === tab ? 'true' : 'false'"
        :class="activeTab === tab ? 'bg-mnema-accent/15 text-mnema-accent font-semibold border-mnema-accent/30' : 'text-mnema-muted hover:text-mnema-text border-transparent'"
        class="px-3 py-1 rounded-md border transition whitespace-nowrap"
        @click="activeTab = tab"
      >
        {{ $t(`legal.tabs.${tab}`) }}
      </button>
    </div>

    <!-- Scrollable document body -->
    <div class="p-6 overflow-y-auto space-y-6 text-sm leading-relaxed text-mnema-muted">
      <!-- 1. Operator -->
      <section v-if="activeTab === 'all' || activeTab === 'operator'" class="space-y-3">
        <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
          <Server class="w-4 h-4 text-mnema-accent" />
          <h3>{{ $t('legal.operator.heading') }}</h3>
        </div>
        <p>
          <strong class="text-mnema-text">{{ $t('legal.operator.introName') }}</strong>{{ $t('legal.operator.introAfter') }}
        </p>
        <div class="bg-mnema-surface border border-mnema-border rounded-xl p-4 grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
          <div>
            <span class="block text-mnema-tertiary uppercase font-mono tracking-wider text-xs">{{ $t('legal.operator.controller') }}</span>
            <span class="font-medium text-mnema-text">{{ operator.name }}</span>
          </div>
          <div>
            <span class="block text-mnema-tertiary uppercase font-mono tracking-wider text-xs">{{ $t('legal.operator.email') }}</span>
            <a :href="`mailto:${operator.email}`" class="font-medium text-mnema-accent hover:underline">{{ operator.email }}</a>
          </div>
          <div>
            <span class="block text-mnema-tertiary uppercase font-mono tracking-wider text-xs">{{ $t('legal.operator.country') }}</span>
            <span class="text-mnema-text">{{ operator.country }}</span>
          </div>
          <div>
            <span class="block text-mnema-tertiary uppercase font-mono tracking-wider text-xs">{{ $t('legal.operator.status') }}</span>
            <span class="text-mnema-text">{{ $t('legal.operator.statusNote', { status: operator.status }) }}</span>
          </div>
        </div>
      </section>

      <!-- 2. Privacy notice -->
      <section v-if="activeTab === 'all' || activeTab === 'privacy'" class="space-y-4">
        <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
          <Lock class="w-4 h-4 text-mnema-accent" />
          <h3>{{ $t('legal.privacy.heading') }}</h3>
        </div>
        <p>
          {{ $t('legal.privacy.introBefore') }}<strong class="text-mnema-text">{{ $t('legal.privacy.introStrong') }}</strong>{{ $t('legal.privacy.introAfter') }}
        </p>

        <!-- Legal bases -->
        <div class="space-y-1.5">
          <h4 class="font-semibold text-mnema-text text-sm">{{ $t('legal.privacy.basisHeading') }}</h4>
          <ul class="list-disc pl-5 space-y-1 text-xs">
            <li><strong class="text-mnema-text">{{ $t('legal.privacy.basisConsentTitle') }}</strong>{{ $t('legal.privacy.basisConsent') }}</li>
            <li><strong class="text-mnema-text">{{ $t('legal.privacy.basisContractTitle') }}</strong>{{ $t('legal.privacy.basisContract') }}</li>
            <li><strong class="text-mnema-text">{{ $t('legal.privacy.basisInterestTitle') }}</strong>{{ $t('legal.privacy.basisInterest') }}</li>
          </ul>
        </div>

        <!-- Voice and calls: everything is routed through the server -->
        <div class="bg-mnema-surface/70 border border-mnema-accent/30 rounded-xl p-4 space-y-2">
          <h4 class="font-semibold text-mnema-accent text-sm flex items-center gap-1.5">
            <ShieldCheck class="w-4 h-4" />
            <span>{{ $t('legal.privacy.voiceHeading') }}</span>
          </h4>
          <p class="text-xs">
            {{ $t('legal.privacy.voiceBefore') }}<strong class="text-mnema-text">{{ $t('legal.privacy.voiceSfu') }}</strong>{{ $t('legal.privacy.voiceMid') }}<strong class="text-mnema-text">{{ $t('legal.privacy.voiceNoP2p') }}</strong>{{ $t('legal.privacy.voiceAfter1') }}<strong class="text-mnema-text">{{ $t('legal.privacy.voiceNoRecord') }}</strong>{{ $t('legal.privacy.voiceAfter2') }}
          </p>
          <p v-if="stunServers.length" class="text-xs">
            <strong class="text-mnema-text">{{ $t('legal.privacy.stunTitle') }}</strong>{{ $t('legal.privacy.stunBefore') }}<span class="font-mono text-mnema-text">{{ stunServers.join(', ') }}</span>{{ $t('legal.privacy.stunAfter') }}
          </p>
          <p v-else class="text-xs">
            {{ $t('legal.privacy.noStun') }}
          </p>
        </div>

        <!-- Data categories & retention -->
        <div class="space-y-2">
          <h4 class="font-semibold text-mnema-text text-sm">{{ $t('legal.privacy.dataHeading') }}</h4>
          <div class="border border-mnema-border rounded-lg overflow-hidden">
            <table class="w-full text-left text-xs">
              <thead class="bg-mnema-surface font-mono text-xs text-mnema-tertiary uppercase">
                <tr>
                  <th class="p-2.5">{{ $t('legal.privacy.colCategory') }}</th>
                  <th class="p-2.5">{{ $t('legal.privacy.colPurpose') }}</th>
                  <th class="p-2.5">{{ $t('legal.privacy.colRetention') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-mnema-hairline">
                <tr>
                  <td class="p-2.5 font-medium text-mnema-text">{{ $t('legal.privacy.account') }}</td>
                  <td class="p-2.5">{{ $t('legal.privacy.accountPurpose') }}</td>
                  <td class="p-2.5">{{ $t('legal.privacy.accountRetention') }}</td>
                </tr>
                <tr>
                  <td class="p-2.5 font-medium text-mnema-text">{{ $t('legal.privacy.messages') }}</td>
                  <td class="p-2.5">{{ $t('legal.privacy.messagesPurpose') }}</td>
                  <td class="p-2.5">{{ $t('legal.privacy.messagesRetention') }}</td>
                </tr>
                <tr>
                  <td class="p-2.5 font-medium text-mnema-text">{{ $t('legal.privacy.media') }}</td>
                  <td class="p-2.5">{{ $t('legal.privacy.mediaPurpose') }}</td>
                  <td class="p-2.5">{{ mediaRetentionDays > 0 ? $t('legal.privacy.mediaRetentionDays', { count: mediaRetentionDays }) : $t('legal.privacy.mediaRetentionManual') }}</td>
                </tr>
                <tr>
                  <td class="p-2.5 font-medium text-mnema-text">{{ $t('legal.privacy.session') }}</td>
                  <td class="p-2.5">{{ $t('legal.privacy.sessionPurpose') }}</td>
                  <td class="p-2.5">{{ $t('legal.privacy.sessionRetention', { days: sessionExpiryDays }) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- Your rights -->
        <div class="space-y-1.5">
          <h4 class="font-semibold text-mnema-text text-sm">{{ $t('legal.privacy.rightsHeading') }}</h4>
          <p class="text-xs">
            {{ $t('legal.privacy.rights1Before') }}<strong class="text-mnema-text">{{ $t('legal.privacy.rightsAccess') }}</strong>{{ $t('legal.privacy.rights1Mid1') }}<strong class="text-mnema-text">{{ $t('legal.privacy.rightsRectify') }}</strong>{{ $t('legal.privacy.rights1Mid2') }}<strong class="text-mnema-text">{{ $t('legal.privacy.rightsErase') }}</strong>{{ $t('legal.privacy.rights1Mid3') }}<strong class="text-mnema-text">{{ $t('legal.privacy.rightsRestrict') }}</strong>{{ $t('legal.privacy.rights1Mid4') }}<strong class="text-mnema-text">{{ $t('legal.privacy.rightsPortability') }}</strong>{{ $t('legal.privacy.rights1Mid5') }}<a :href="`mailto:${operator.email}`" class="text-mnema-accent hover:underline">{{ operator.email }}</a>{{ $t('legal.privacy.rights1After') }}
          </p>
          <p class="text-xs">
            {{ $t('legal.privacy.rights2Before') }}<strong class="text-mnema-text">{{ $t('legal.privacy.rightsComplain') }}</strong>{{ $t('legal.privacy.rights2After') }}
          </p>
        </div>
      </section>

      <!-- 3. Cookies & local storage -->
      <section v-if="activeTab === 'all' || activeTab === 'privacy'" class="space-y-3">
        <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
          <Cookie class="w-4 h-4 text-mnema-accent" />
          <h3>{{ $t('legal.cookies.heading') }}</h3>
        </div>
        <p>{{ $t('legal.cookies.body') }}</p>
      </section>

      <!-- 4. Terms of use -->
      <section v-if="activeTab === 'all' || activeTab === 'terms'" class="space-y-3">
        <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
          <Scale class="w-4 h-4 text-mnema-accent" />
          <h3>{{ $t('legal.terms.heading') }}</h3>
        </div>
        <div class="space-y-2 text-xs">
          <p><strong class="text-mnema-text">{{ $t('legal.terms.useTitle') }}</strong>{{ $t('legal.terms.use') }}</p>
          <p><strong class="text-mnema-text">{{ $t('legal.terms.liabilityTitle') }}</strong>{{ $t('legal.terms.liabilityBefore') }}<em class="italic">{{ $t('legal.terms.liabilityAsIs') }}</em>{{ $t('legal.terms.liabilityAfter') }}</p>
          <p><strong class="text-mnema-text">{{ $t('legal.terms.rulesTitle') }}</strong>{{ $t('legal.terms.rules') }}</p>
        </div>
      </section>

      <!-- 5. Open-source credits -->
      <section v-if="activeTab === 'all'" class="space-y-3 border-t border-mnema-hairline pt-4">
        <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
          <Code class="w-4 h-4 text-mnema-accent" />
          <h3>{{ $t('legal.oss.heading') }}</h3>
        </div>
        <p class="text-xs">{{ $t('legal.oss.intro') }}</p>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs">
          <div
            v-for="lib in OSS_LIBS"
            :key="lib.name"
            class="flex items-center justify-between p-2 rounded-lg bg-mnema-surface border border-mnema-hairline"
          >
            <div class="flex items-center gap-1.5 min-w-0">
              <span class="font-medium text-mnema-text truncate">{{ lib.name }}</span>
              <span class="text-xs text-mnema-tertiary font-mono">({{ lib.license }})</span>
            </div>
            <a
              :href="lib.url"
              target="_blank"
              rel="noopener"
              v-tooltip="$t('legal.oss.openSite', { name: lib.name })"
              class="text-mnema-tertiary hover:text-mnema-accent transition flex-shrink-0"
            >
              <ExternalLink class="w-4 h-4" />
            </a>
          </div>
        </div>
      </section>
    </div>

    <!-- Footer -->
    <div class="px-6 py-3 border-t border-mnema-hairline bg-mnema-surface/50 flex items-center justify-between gap-3 flex-shrink-0">
      <span class="text-xs text-mnema-tertiary truncate">
        {{ $t('legal.footer', { year: new Date().getFullYear(), name: operator.name }) }}
      </span>
      <button
        type="button"
        class="px-4 py-1.5 rounded-md bg-mnema-accent hover:bg-mnema-accent-hover text-mnema-accent-ink text-sm font-semibold transition shadow-sm flex-shrink-0"
        @click="emit('close')"
      >
        {{ $t('common.close') }}
      </button>
    </div>
  </BaseDialog>
</template>
