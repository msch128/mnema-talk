// Operator warning codes from the server, with localized text and the matching operations doc link.
import { t } from '../i18n'
import { SOURCE_URL } from './thirdParty'

const OPERATIONS_DOC = `${SOURCE_URL}/blob/main/doc/operations.md`
const PROXY_DOC = `${OPERATIONS_DOC}#reverse-proxy-and-trusted_proxy_cidrs`
const VOICE_DOC = `${OPERATIONS_DOC}#nobody-can-join-voice-or-it-connects-and-drops-after-a-few-seconds`

export function describeWarning(code: string, value: number | undefined) {
  switch (code) {
    case 'trusted_proxy_broad':
      return { title: t('admin.system.warn.trusted_proxy_broad.title'), body: t('admin.system.warn.trusted_proxy_broad.body'), doc: PROXY_DOC }
    case 'proxy_not_trusted':
      return { title: t('admin.system.warn.proxy_not_trusted.title'), body: t('admin.system.warn.proxy_not_trusted.body'), doc: PROXY_DOC }
    case 'webrtc_reachability':
      return { title: t('admin.system.warn.webrtc_reachability.title'), body: t('admin.system.warn.webrtc_reachability.body'), doc: VOICE_DOC }
    case 'webrtc_udp_mux_disabled':
      return { title: t('admin.system.warn.webrtc_udp_mux_disabled.title'), body: t('admin.system.warn.webrtc_udp_mux_disabled.body', { count: value ?? 0 }), doc: `${OPERATIONS_DOC}#voice-webrtc` }
    case 'insecure_public_url':
      return { title: t('admin.system.warn.insecure_public_url.title'), body: t('admin.system.warn.insecure_public_url.body'), doc: `${OPERATIONS_DOC}#public_url-and-secure-cookies` }
    case 'voice_unavailable':
      return { title: t('admin.system.warn.voice_unavailable.title'), body: t('admin.system.warn.voice_unavailable.body'), doc: VOICE_DOC }
    case 'turn_unavailable':
      return { title: t('admin.system.warn.turn_unavailable.title'), body: t('admin.system.warn.turn_unavailable.body', { port: value ?? 0 }), doc: `${OPERATIONS_DOC}#built-in-turn-relay` }
    default:
      return { title: t('admin.system.warn.unknown.title', { code }), body: t('admin.system.warn.unknown.body'), doc: OPERATIONS_DOC }
  }
}
