<script setup>
import { ref, onMounted } from 'vue'
import { X, ShieldCheck, Scale, Server, Lock, Cookie, Code, ExternalLink } from '@lucide/vue'

const emit = defineEmits(['close'])

const activeTab = ref('all') // 'all', 'privacy', 'terms', 'operator'

const operator = ref({
  name: 'Community Operator',
  email: 'admin@example.com',
  country: 'Deutschland',
  status: 'Privates, nicht-kommerzielles Projekt'
})
const mediaRetentionDays = ref(0) // 0 = no automatic deletion
const sessionExpiryDays = ref(30)
const stunServers = ref([])
const legalVersion = ref('1.3')

onMounted(async () => {
  try {
    const res = await fetch('/api/legal')
    if (res.ok) {
      const data = await res.json()
      operator.value = {
        name: data.operator_name || 'Community Operator',
        email: data.operator_email || 'admin@example.com',
        country: data.operator_country || 'Deutschland',
        status: data.project_notice || 'Privates, nicht-kommerzielles Projekt'
      }
      mediaRetentionDays.value = data.media_retention_days || 0
      if (data.session_expiry_days) sessionExpiryDays.value = data.session_expiry_days
      stunServers.value = data.stun_servers || []
      if (data.legal_version) legalVersion.value = data.legal_version
    }
  } catch (e) {
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
  <div class="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-6 bg-black/80 backdrop-blur-none animate-fadeIn">
    <div 
      class="bg-mnema-elevated border border-mnema-border w-full max-w-3xl max-h-[90vh] rounded-2xl shadow-2xl flex flex-col overflow-hidden text-mnema-text"
      @click.stop
    >
      <!-- Modal Header -->
      <div class="px-6 py-4 border-b border-mnema-hairline flex items-center justify-between flex-shrink-0 bg-mnema-surface/50">
        <div class="flex items-center gap-3">
          <div class="w-8 h-8 rounded-lg bg-mnema-band border border-mnema-mint/30 flex items-center justify-center text-mnema-mint">
            <ShieldCheck class="w-4 h-4" />
          </div>
          <div>
            <h2 class="text-lg font-bold tracking-tight text-mnema-text flex items-center gap-2">
              <span>Rechtliches & Datenschutzerklärung</span>
              <span class="text-xs font-mono font-normal text-mnema-tertiary bg-mnema-surface px-1.5 py-0.5 rounded border border-mnema-border">v{{ legalVersion }} (DSGVO)</span>
            </h2>
            <p class="text-xs text-mnema-muted">
              Transparenz, Datenschutz (Art. 13 DSGVO) & Nutzungsbedingungen für Mnema Talk
            </p>
          </div>
        </div>

        <button 
          @click="emit('close')"
          class="p-1.5 rounded-lg text-mnema-tertiary hover:text-mnema-text hover:bg-mnema-surface transition"
          title="Schließen"
        >
          <X class="w-5 h-5" />
        </button>
      </div>

      <!-- Navigation Tabs -->
      <div class="flex items-center gap-1 px-6 py-2 border-b border-mnema-hairline bg-mnema-canvas/40 text-sm overflow-x-auto flex-shrink-0">
        <button 
          @click="activeTab = 'all'"
          :class="activeTab === 'all' ? 'bg-mnema-accent/15 text-mnema-accent font-semibold border-mnema-accent/30' : 'text-mnema-muted hover:text-mnema-text border-transparent'"
          class="px-3 py-1 rounded-md border transition whitespace-nowrap"
        >
          Gesamtübersicht
        </button>
        <button 
          @click="activeTab = 'operator'"
          :class="activeTab === 'operator' ? 'bg-mnema-accent/15 text-mnema-accent font-semibold border-mnema-accent/30' : 'text-mnema-muted hover:text-mnema-text border-transparent'"
          class="px-3 py-1 rounded-md border transition whitespace-nowrap"
        >
          Betreiber / Verantwortlicher
        </button>
        <button 
          @click="activeTab = 'privacy'"
          :class="activeTab === 'privacy' ? 'bg-mnema-accent/15 text-mnema-accent font-semibold border-mnema-accent/30' : 'text-mnema-muted hover:text-mnema-text border-transparent'"
          class="px-3 py-1 rounded-md border transition whitespace-nowrap"
        >
          Datenschutz (DSGVO)
        </button>
        <button 
          @click="activeTab = 'terms'"
          :class="activeTab === 'terms' ? 'bg-mnema-accent/15 text-mnema-accent font-semibold border-mnema-accent/30' : 'text-mnema-muted hover:text-mnema-text border-transparent'"
          class="px-3 py-1 rounded-md border transition whitespace-nowrap"
        >
          Nutzungsbedingungen
        </button>
      </div>

      <!-- Scrollable Document Body -->
      <div class="p-6 overflow-y-auto space-y-6 text-sm leading-relaxed text-mnema-muted">
        <!-- 1. Kurzfassung & Betreiber -->
        <section v-if="activeTab === 'all' || activeTab === 'operator'" class="space-y-3">
          <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
            <Server class="w-4 h-4 text-mnema-accent" />
            <h3>1. Betreiber & Verantwortlicher</h3>
          </div>
          <p>
            <strong class="text-mnema-text">Mnema Talk</strong> ist ein privates, nicht-kommerzielles Softwareprojekt für geschlossene Gemeinschaften – ohne Gewinnerzielungsabsicht, ohne Werbeeinnahmen und ohne geschäftliche Beziehung. Konten werden ausschließlich über persönliche Einladungsschlüssel vergeben.
          </p>
          <div class="bg-mnema-surface border border-mnema-border rounded-xl p-4 grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
            <div>
              <span class="block text-mnema-tertiary uppercase font-mono tracking-wider text-xs">Verantwortlicher (Art. 4 Nr. 7 DSGVO)</span>
              <span class="font-medium text-mnema-text">{{ operator.name }}</span>
            </div>
            <div>
              <span class="block text-mnema-tertiary uppercase font-mono tracking-wider text-xs">E-Mail-Kontakt</span>
              <a :href="`mailto:${operator.email}`" class="font-medium text-mnema-accent hover:underline">{{ operator.email }}</a>
            </div>
            <div>
              <span class="block text-mnema-tertiary uppercase font-mono tracking-wider text-xs">Land / Gerichtsstand</span>
              <span class="text-mnema-text">{{ operator.country }}</span>
            </div>
            <div>
              <span class="block text-mnema-tertiary uppercase font-mono tracking-wider text-xs">Projektstatus</span>
              <span class="text-mnema-text">{{ operator.status }} (keine § 5 DDG Impressumspflicht)</span>
            </div>
          </div>
        </section>

        <!-- 2. Datenschutzerklärung -->
        <section v-if="activeTab === 'all' || activeTab === 'privacy'" class="space-y-4">
          <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
            <Lock class="w-4 h-4 text-mnema-accent" />
            <h3>2. Datenschutzerklärung nach Art. 13 DSGVO</h3>
          </div>
          <p>
            Ihre Privatsphäre und Datensouveränität stehen im Zentrum. Es wird ausschließlich das technische Minimum erhoben, das für den zuverlässigen Chat-, Sprach- und Video-Betrieb erforderlich ist. Ihre Daten werden <strong class="text-mnema-text">weder verkauft, noch an Werbetreibende weitergegeben, noch zum Training von KI-Modellen verwendet</strong>.
          </p>

          <!-- Rechtsgrundlagen -->
          <div class="space-y-1.5">
            <h4 class="font-semibold text-mnema-text text-sm">Rechtsgrundlagen der Verarbeitung</h4>
            <ul class="list-disc pl-5 space-y-1 text-xs">
              <li><strong class="text-mnema-text">Einwilligung (Art. 6 Abs. 1 lit. a DSGVO)</strong>: Durch Ihre Registrierung und Nutzung des Einladungscodes willigen Sie in die Speicherung Ihrer Profildaten und hochgeladenen Medien ein.</li>
              <li><strong class="text-mnema-text">Vertragserfüllung / Nutzungsverhältnis (Art. 6 Abs. 1 lit. b DSGVO)</strong>: Zur Bereitstellung des Chat- und Sprachdienstes, Authentifizierung sowie Sitzungsverwaltung.</li>
              <li><strong class="text-mnema-text">Berechtigtes Interesse (Art. 6 Abs. 1 lit. f DSGVO)</strong>: Kurzlebige Fehler- und Zugriffsprotokolle zur Absicherung gegen Missbrauch und Brute-Force-Angriffe.</li>
            </ul>
          </div>

          <!-- WebRTC Voice & Calls: Strenge SFU-Architektur -->
          <div class="bg-mnema-surface/70 border border-mnema-accent/30 rounded-xl p-4 space-y-2">
            <h4 class="font-semibold text-mnema-accent text-sm flex items-center gap-1.5">
              <ShieldCheck class="w-4 h-4" />
              <span>WebRTC Voice, Video & Screen Sharing: 100% Server-geroutet</span>
            </h4>
            <p class="text-xs">
              Im Gegensatz zu herkömmlichen Chat-Apps setzt Mnema Talk eine dedizierte <strong class="text-mnema-text">Pion WebRTC SFU</strong> auf dem eigenen Server ein. 
              <strong class="text-mnema-text">Es finden keinerlei Direkt-P2P-Verbindungen zwischen Teilnehmern statt.</strong> Dadurch wird Ihre IP-Adresse niemals an andere Gesprächsteilnehmer offengelegt. Audioströme und Screenshares werden in Echtzeit selektiv im Arbeitsspeicher weitergeleitet und <strong class="text-mnema-text">zu keinem Zeitpunkt aufgezeichnet oder dauerhaft gespeichert</strong>.
            </p>
            <p v-if="stunServers.length" class="text-xs">
              <strong class="text-mnema-text">STUN-Server:</strong> Zum Aufbau der Sprachverbindung fragt Ihr Browser folgende STUN-Server nach Ihrer öffentlichen Adresse:
              <span class="font-mono text-mnema-text">{{ stunServers.join(', ') }}</span>.
              Dabei wird Ihre IP-Adresse an den jeweiligen Betreiber dieser Server übermittelt; Inhalte von Gesprächen oder Nachrichten werden nicht übertragen.
            </p>
            <p v-else class="text-xs">
              Es werden keine externen STUN- oder TURN-Server von Drittanbietern verwendet.
            </p>
          </div>

          <!-- Datenkategorien & Speicherdauer -->
          <div class="space-y-2">
            <h4 class="font-semibold text-mnema-text text-sm">Kategorien & Speicherdauer</h4>
            <div class="border border-mnema-border rounded-lg overflow-hidden">
              <table class="w-full text-left text-xs">
                <thead class="bg-mnema-surface font-mono text-xs text-mnema-tertiary uppercase">
                  <tr>
                    <th class="p-2.5">Datenkategorie</th>
                    <th class="p-2.5">Zweck</th>
                    <th class="p-2.5">Aufbewahrung</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-mnema-hairline">
                  <tr>
                    <td class="p-2.5 font-medium text-mnema-text">Benutzerkonto</td>
                    <td class="p-2.5">Benutzername, Anzeigename, bcrypt-Passwort-Hash</td>
                    <td class="p-2.5">Bis zur Kontolöschung</td>
                  </tr>
                  <tr>
                    <td class="p-2.5 font-medium text-mnema-text">Chat-Nachrichten</td>
                    <td class="p-2.5">Textnachrichten, Threads, Emoji-Reaktionen</td>
                    <td class="p-2.5">Dauerhaft bis zur manuellen Löschung</td>
                  </tr>
                  <tr>
                    <td class="p-2.5 font-medium text-mnema-text">Medien-Uploads</td>
                    <td class="p-2.5">Bilder, Dokumente, Avatare im lokalen S3-Speicher</td>
                    <td class="p-2.5">{{ mediaRetentionDays > 0 ? `${mediaRetentionDays} Tage, danach automatisch gelöscht` : 'Bis zur manuellen Löschung' }}</td>
                  </tr>
                  <tr>
                    <td class="p-2.5 font-medium text-mnema-text">Sitzungstoken</td>
                    <td class="p-2.5">Signierte JWT-Tokens zur Authentifizierung</td>
                    <td class="p-2.5">Max. {{ sessionExpiryDays }} Tage oder bis zum Logout</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <!-- Ihre Rechte -->
          <div class="space-y-1.5">
            <h4 class="font-semibold text-mnema-text text-sm">Ihre Rechte als betroffene Person</h4>
            <p class="text-xs">
              Sie haben nach Art. 15–22 DSGVO jederzeit das Recht auf unentgeltliche <strong class="text-mnema-text">Auskunft</strong> über Ihre gespeicherten Daten, <strong class="text-mnema-text">Berichtigung</strong> unrichtiger Daten, <strong class="text-mnema-text">Löschung</strong> Ihres Kontos, <strong class="text-mnema-text">Einschränkung</strong> der Verarbeitung sowie das Recht auf <strong class="text-mnema-text">Datenübertragbarkeit</strong>. Wenden Sie sich zur Ausübung formlos an <a :href="`mailto:${operator.email}`" class="text-mnema-accent hover:underline">{{ operator.email }}</a>.
            </p>
            <p class="text-xs">
              Außerdem können Sie einer Verarbeitung auf Grundlage berechtigter Interessen widersprechen (Art. 21 DSGVO) und eine erteilte Einwilligung jederzeit mit Wirkung für die Zukunft widerrufen (Art. 7 Abs. 3 DSGVO).
              Sie haben zudem das Recht, sich bei einer <strong class="text-mnema-text">Datenschutzaufsichtsbehörde zu beschweren</strong> (Art. 77 DSGVO), insbesondere in dem Land Ihres Wohnorts oder des Wohnsitzes des Betreibers.
            </p>
          </div>
        </section>

        <!-- 3. Cookies & Lokaler Speicher -->
        <section v-if="activeTab === 'all' || activeTab === 'privacy'" class="space-y-3">
          <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
            <Cookie class="w-4 h-4 text-mnema-accent" />
            <h3>3. Cookies & Lokaler Speicher</h3>
          </div>
          <p>
            Mnema Talk verzichtet vollständig auf Marketing-, Tracking- und Drittanbieter-Cookies. Gemäß § 25 Abs. 2 TDDDG werden ausschließlich technisch zwingend notwendige Sitzungsdaten (JWT-Authentifizierung) sowie lokale Einstellungen (z. B. Audio-Mikrofonpegel, Noise-Gate-Schwellenwerte) in Ihrem Browser gespeichert. Ein Cookie-Banner ist daher rechtlich nicht erforderlich.
          </p>
        </section>

        <!-- 4. Nutzungsbedingungen -->
        <section v-if="activeTab === 'all' || activeTab === 'terms'" class="space-y-3">
          <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
            <Scale class="w-4 h-4 text-mnema-accent" />
            <h3>4. Nutzungsbedingungen (Terms of Service)</h3>
          </div>
          <div class="space-y-2 text-xs">
            <p>
              <strong class="text-mnema-text">Zulässige Nutzung:</strong> Die Plattform dient dem privaten Austausch. Nutzer verpflichten sich, keine rechtswidrigen, beleidigenden, volksverhetzenden oder urheberrechtsverletzenden Inhalte zu teilen.
            </p>
            <p>
              <strong class="text-mnema-text">Haftungsausschluss:</strong> Da es sich um ein unentgeltliches, privates Freizeitprojekt handelt, erfolgt die Bereitstellung „wie besehen“ (<em class="italic">as-is</em>) ohne Garantie auf permanente Verfügbarkeit oder Fehlerfreiheit.
            </p>
            <p>
              <strong class="text-mnema-text">Hausrecht & Beendigung:</strong> Der Betreiber behält sich vor, Konten bei groben Verstößen gegen den respektvollen Umgang oder bei Sicherheitsgefährdungen der Infrastruktur ohne Vorankündigung zu sperren.
            </p>
          </div>
        </section>

        <!-- 5. Open Source Lizenzen -->
        <section v-if="activeTab === 'all'" class="space-y-3 border-t border-mnema-hairline pt-4">
          <div class="flex items-center gap-2 text-mnema-text font-semibold text-base">
            <Code class="w-4 h-4 text-mnema-accent" />
            <h3>5. Open-Source-Attribution</h3>
          </div>
          <p class="text-xs">
            Mnema Talk baut auf bewährter freier Software auf.
          </p>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs">
            <div 
              v-for="lib in OSS_LIBS" 
              :key="lib.name"
              class="flex items-center justify-between p-2 rounded-lg bg-mnema-surface border border-mnema-hairline"
            >
              <div class="flex items-center gap-1.5">
                <span class="font-medium text-mnema-text">{{ lib.name }}</span>
                <span class="text-xs text-mnema-tertiary font-mono">({{ lib.license }})</span>
              </div>
              <a 
                :href="lib.url" 
                target="_blank" 
                rel="noopener"
                class="text-mnema-tertiary hover:text-mnema-accent transition"
              >
                <ExternalLink class="w-4 h-4" />
              </a>
            </div>
          </div>
        </section>
      </div>

      <!-- Modal Footer -->
      <div class="px-6 py-3 border-t border-mnema-hairline bg-mnema-surface/50 flex items-center justify-between flex-shrink-0">
        <span class="text-xs text-mnema-tertiary">
          © {{ new Date().getFullYear() }} {{ operator.name }} · Alle Rechte vorbehalten
        </span>
        <button 
          @click="emit('close')"
          class="px-4 py-1.5 rounded-lg bg-mnema-accent hover:bg-mnema-accent-hover text-mnema-accent-ink text-sm font-semibold transition shadow-sm"
        >
          Schließen
        </button>
      </div>
    </div>
  </div>
</template>
