// Open-source software that Mnema Talk is built from or ships, shown in the
// legal dialog. The full list with every Go module, version and license text
// location is THIRD_PARTY_NOTICES.md; keep both in sync when dependencies
// change (thirdParty.test.js checks the web dependencies).

// AGPL-3.0 section 13: users of a network deployment get the source. A modified
// deployment must point this at its own source.
export const SOURCE_URL = 'https://github.com/msch128/mnema-talk'
export const NOTICES_FILE = 'THIRD_PARTY_NOTICES.md'
export const NOTICES_URL = `${SOURCE_URL}/blob/main/${NOTICES_FILE}`

export const THIRD_PARTY = [
  {
    group: 'browser',
    items: [
      { name: 'Vue', pkg: 'vue', license: 'MIT', url: 'https://vuejs.org' },
      { name: 'Pinia', pkg: 'pinia', license: 'MIT', url: 'https://pinia.vuejs.org' },
      { name: 'Lucide Icons', pkg: '@lucide/vue', license: 'ISC', url: 'https://lucide.dev' },
      { name: 'Inter', pkg: '@fontsource/inter', license: 'OFL-1.1', url: 'https://rsms.me/inter/' },
      { name: 'JetBrains Mono', pkg: '@fontsource/jetbrains-mono', license: 'OFL-1.1', url: 'https://www.jetbrains.com/lp/mono/' },
      { name: 'emoji-picker-element', pkg: 'emoji-picker-element', license: 'Apache-2.0', url: 'https://github.com/nolanlawson/emoji-picker-element' },
      { name: 'emoji-picker-element-data (CLDR, Emojibase)', pkg: 'emoji-picker-element-data', license: 'Apache-2.0', url: 'https://github.com/nolanlawson/emoji-picker-element-data' },
      { name: 'Swagger UI (API reference)', pkg: 'swagger-ui-dist', license: 'Apache-2.0', url: 'https://github.com/swagger-api/swagger-ui' },
      { name: 'Tailwind CSS', license: 'MIT', url: 'https://tailwindcss.com', note: 'generated' }
    ]
  },
  {
    group: 'noise',
    items: [
      { name: 'DeepFilterNet3', license: 'MIT OR Apache-2.0', url: 'https://github.com/Rikorose/DeepFilterNet', note: 'dfn' },
      { name: 'mezon-noise-suppression (adapted WASM glue)', license: 'MIT OR Apache-2.0', url: 'https://github.com/mezonai/mezon-noise-suppression', note: 'dfnBuild' },
      { name: 'tract & Rust crates', license: 'MIT / Apache-2.0', url: 'https://github.com/sonos/tract', note: 'crates' },
      { name: 'web-noise-suppressor', pkg: '@sapphi-red/web-noise-suppressor', license: 'MIT', url: 'https://github.com/sapphi-red/web-noise-suppressor' },
      { name: 'GTCRN', license: 'MIT', url: 'https://github.com/Xiaobin-Rong/gtcrn' },
      { name: 'PFFFT', license: 'BSD-style (FFTPACK)', url: 'https://github.com/marton78/pffft' }
    ]
  },
  {
    group: 'server',
    items: [
      { name: 'Go', license: 'BSD-3-Clause', url: 'https://go.dev' },
      { name: 'Pion WebRTC', license: 'MIT', url: 'https://pion.ly' },
      { name: 'pgx', license: 'MIT', url: 'https://github.com/jackc/pgx' },
      { name: 'chi', license: 'MIT', url: 'https://github.com/go-chi/chi' },
      { name: 'Gorilla WebSocket', license: 'BSD-2-Clause', url: 'https://github.com/gorilla/websocket' },
      { name: 'AWS SDK for Go v2', license: 'Apache-2.0', url: 'https://github.com/aws/aws-sdk-go-v2' },
      { name: 'golang-jwt', license: 'MIT', url: 'https://github.com/golang-jwt/jwt' },
      { name: 'google/uuid', license: 'BSD-3-Clause', url: 'https://github.com/google/uuid' },
      { name: 'godotenv', license: 'MIT', url: 'https://github.com/joho/godotenv' },
      { name: 'Go x/ libraries', license: 'BSD-3-Clause', url: 'https://pkg.go.dev/golang.org/x' }
    ]
  },
  {
    group: 'services',
    items: [
      { name: 'PostgreSQL 18', license: 'PostgreSQL License', url: 'https://www.postgresql.org' },
      { name: 'SeaweedFS', license: 'Apache-2.0', url: 'https://github.com/seaweedfs/seaweedfs' },
      { name: 'coturn', license: 'BSD-3-Clause', url: 'https://github.com/coturn/coturn', note: 'turn' },
      { name: 'Watchtower', license: 'Apache-2.0', url: 'https://github.com/nicholas-fedor/watchtower', note: 'updater' },
      { name: 'Alpine Linux', license: 'GPL-2.0 and others', url: 'https://alpinelinux.org', note: 'image' }
    ]
  }
]
