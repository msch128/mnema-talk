// Import zones of web/src: which folder may import which.
//
// REVIEW NOTE: the tables below are the contract of the feature folders, the
// counterpart of internal/repo/boundaries_test.go for the Go packages. Adding
// an edge or an exception is an architecture decision: say why in the pull
// request. A feature imports shared code and its own files; what two features
// need moves down into shared, or the shell (app) wires them together.
import { dirname, relative, resolve, sep } from 'node:path'

export const FEATURES = ['chat', 'talk', 'dm', 'forum', 'events', 'wiki', 'admin', 'settings'] as const
export type Feature = typeof FEATURES[number]
/** A feature, the code every feature may use, or the shell that composes them. */
export type Zone = Feature | 'shared' | 'app'

/**
 * The other features a feature may import, besides shared and itself. Every
 * edge points one way, so the features stay a layered graph without a cycle.
 */
export const ALLOWED: Record<Feature, readonly Feature[]> = {
  talk: [],
  // A call in a conversation runs on the Talk engine.
  dm: ['talk'],
  // The chat area shows conversations as well as channels, and messages carry Talk invitations.
  chat: ['dm', 'talk'],
  // A forum post is a thread: it reuses the composer, replies and search of chat.
  forum: ['chat'],
  // An event starts in a Talk.
  events: ['talk'],
  wiki: [],
  admin: [],
  // The audio tab of the settings dialog belongs to Talk.
  settings: ['talk']
}

/**
 * Single modules that may be imported across the table above. Each entry
 * names a module that sits in the wrong layer for one of its exports; the
 * reason says what would remove the entry.
 */
export const EXCEPTIONS: readonly { target: string; from: readonly Zone[]; reason: string }[] = [
  { target: 'chat/composables/useNavMenus', from: ['shared', 'talk'], reason: 'holds useMenuState (the one open menu) next to the member and channel menus; goes away when useMenuState moves to shared' },
  { target: 'talk/composables/useTalkStage', from: ['shared'], reason: 'presentationTypes re-exports the TalkParticipant type; goes away when the type moves to shared' },
  { target: 'wiki/lib/wikiLinks', from: ['shared'], reason: 'MarkdownContent renders wiki links wherever Markdown is shown' },
  { target: 'wiki/lib/wikiNav', from: ['shared'], reason: 'MarkdownContent opens the wiki page a link names' },
  { target: 'wiki/lib/wikiApi', from: ['shared'], reason: 'MarkdownContent takes the page list as a type' },
  { target: 'wiki/lib/wikiPaths', from: ['shared'], reason: 'the router owns every address, the wiki ones included' },
  { target: 'forum/components/ForumPostHeader', from: ['chat'], reason: 'the thread sidebar shows a forum post under its own header' },
  { target: 'talk/components/ConnectionTest', from: ['admin'], reason: 'the status tab offers the same connection test as the audio settings' },
  { target: 'admin/lib/bots', from: ['settings'], reason: 'the session list uses relativeTime; goes away when it moves to shared' }
]

const ZONES: readonly string[] = [...FEATURES, 'shared', 'app']

/** The zone of a path below web/src ('' for stores, types, i18n and the other base folders). */
export function zoneOf(path: string): Zone | '' {
  const parts = path.split('/')
  // lib/media stayed where it was; it is Talk's media encryption.
  if (parts[0] === 'lib' && parts[1] === 'media') return 'talk'
  const first = parts[0] ?? ''
  return parts.length > 1 && ZONES.includes(first) ? first as Zone : ''
}

const isTest = (path: string) => /\.(test|fixture)\.ts$/.test(path)
const stem = (path: string) => path.replace(/\.(ts|vue|js|json|css)$/, '').replace(/[?#].*$/, '')

/**
 * Why `from` must not import `to` (both below web/src), or '' when it may.
 * Tests and fixtures are free: they drive flows across features.
 */
export function zoneViolation(from: string, to: string): string {
  if (isTest(from)) return ''
  const source = zoneOf(from)
  const target = zoneOf(to)
  if (target === '' || target === source || source === 'app') return ''
  // App.vue and main.ts sit directly in web/src and mount the shell.
  if (target === 'app') return from.includes('/') ? 'Nothing imports the shell (app) but App.vue and main.ts: move what is needed into shared.' : ''
  // The base folders (stores, types, directives) carry state and contracts for every feature.
  if (source === '' || target === 'shared') return ''
  if (source !== 'shared' && ALLOWED[source].includes(target as Feature)) return ''
  if (EXCEPTIONS.some(e => e.target === stem(to) && e.from.includes(source))) return ''
  return source === 'shared'
    ? `shared must not import the feature ${target}: it is what every feature builds on.`
    : `The feature ${source} must not import the feature ${target}: use shared, or let the shell wire them (web/scripts/importZones.ts).`
}

/** The same question for an import specifier as written in `file` (absolute path); srcRoot is web/src. */
export function specifierViolation(srcRoot: string, file: string, specifier: string): string {
  if (!specifier.startsWith('.')) return ''
  const from = relative(srcRoot, file).split(sep).join('/')
  const to = relative(srcRoot, resolve(dirname(file), specifier)).split(sep).join('/')
  if (from.startsWith('..') || to.startsWith('..')) return ''
  return zoneViolation(from, to)
}
