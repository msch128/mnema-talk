import { t } from '../i18n'

// The desktop clients (Rust and Electron) report why a server could not be
// opened as an English sentence. This maps the sentences they send to the
// current language and keeps the clients' wording as the contract: a sentence
// this table does not know is shown as sent, so a newer or older host never
// shows nothing. The wording is pinned in hostError.test.ts.

export const NAT_HINT =
  'If the server runs in your own network, your router may not support reaching it through its public address (NAT loopback/hairpin) — try the local address or another network.'

// A trailing "(detail)" is the system's own cause, kept as the host sent it.
const DETAIL = '(?: \\(([^]{1,160})\\))?'

interface Rule {
  match: RegExp
  text: (m: RegExpMatchArray) => string
}

const rules: Rule[] = [
  { match: /^Enter an HTTPS instance address\.$/, text: () => t('nativeDesktop.hostError.enterAddress') },
  { match: /^Invalid instance address\.$/, text: () => t('nativeDesktop.hostError.invalidAddress') },
  { match: /^Use an HTTPS origin without credentials, a path or query\.$/, text: () => t('nativeDesktop.hostError.httpsOrigin') },
  { match: /^Unable to initialize HTTPS\.$/, text: () => t('nativeDesktop.hostError.initHttps') },
  { match: /^The instance refused the connection\. Check the address and the port\.$/, text: () => t('nativeDesktop.hostError.refused') },
  { match: /^The instance did not answer in time\. Check the address, the port and your network\.$/, text: () => t('nativeDesktop.hostError.timeout') },
  { match: /^The instance address could not be resolved\. Check the address and your network\.$/, text: () => t('nativeDesktop.hostError.dns') },
  {
    match: new RegExp(`^This computer does not trust the instance's HTTPS certificate\\. Check the system clock, and that no antivirus or network filter intercepts HTTPS\\.${DETAIL}$`),
    text: (m) => withDetail(t('nativeDesktop.hostError.certificate'), m[1])
  },
  {
    match: new RegExp(`^The instance offers no TLS version this client can use\\. It speaks TLS 1\\.2 and 1\\.3; ask the administrator to enable one of them\\.${DETAIL}$`),
    text: (m) => withDetail(t('nativeDesktop.hostError.tlsVersion'), m[1])
  },
  {
    match: new RegExp(`^Unable to reach the instance using trusted HTTPS\\.${DETAIL}$`),
    text: (m) => withDetail(t('nativeDesktop.hostError.unreachable'), m[1])
  },
  { match: /^The instance is unavailable or redirects to a different address\.$/, text: () => t('nativeDesktop.hostError.unavailable') },
  { match: /^Instance check failed\.$/, text: () => t('nativeDesktop.hostError.checkFailed') },
  { match: /^Invalid instance response\.$/, text: () => t('nativeDesktop.hostError.invalidResponse') },
  { match: /^This instance needs an update for the desktop client\.$/, text: () => t('nativeDesktop.hostError.instanceNeedsUpdate') },
  { match: /^This instance is not compatible with this desktop client\.$/, text: () => t('nativeDesktop.hostError.notCompatible') },
  { match: /^This client has no verified app\. Reinstall the desktop client\.$/, text: () => t('nativeDesktop.hostError.noBundle') },
  { match: /^This server needs a newer Mnema app than this client has\. Update the desktop client\.$/, text: () => t('nativeDesktop.hostError.serverNewer') },
  {
    match: /^This server is too old for this desktop client \(needs ([^()]{1,40}) or newer\)\. Ask the administrator to update it\.$/,
    text: (m) => t('nativeDesktop.hostError.serverTooOld', { version: m[1] })
  },
  { match: /^This server speaks an older contract than any app in this client\. Ask the administrator to update it\.$/, text: () => t('nativeDesktop.hostError.contractRetired') },
  {
    match: /^The app in this client was signed with a key that is no longer trusted\. Update the desktop client; it fetches a newly signed app at its next check\.$/,
    text: () => t('nativeDesktop.hostError.signerWithdrawn')
  },
  {
    match: /^The newest verified app this client has used is no longer available, and older apps are refused\. Update or reinstall the desktop client\.$/,
    text: () => t('nativeDesktop.hostError.rolledBack')
  },
  { match: /^This server is already in the list\.$/, text: () => t('nativeDesktop.hostError.alreadyListed') },
  { match: /^At most (\d{1,3}) servers fit in one window\.$/, text: (m) => t('nativeDesktop.hostError.maxServers', { count: Number(m[1]) }) },
  { match: /^The web app of this server was withdrawn\. Restart the client to get a current one\.$/, text: () => t('nativeDesktop.hostError.withdrawn') },
  { match: /^Several servers at once need macOS 14 or newer\.$/, text: () => t('nativeDesktop.hostError.needsMacos') },
  { match: /^Only the instance selector may open an instance\.$/, text: () => t('nativeDesktop.hostError.selectorOnly') }
]

function withDetail(text: string, detail: string | undefined): string {
  return detail ? `${text} (${detail})` : text
}

/** The host's sentence in the current language; an unknown one as sent. */
export function localizeHostError(sent: string): string {
  const hinted = sent.endsWith(` ${NAT_HINT}`)
  const base = hinted ? sent.slice(0, -(NAT_HINT.length + 1)) : sent
  for (const rule of rules) {
    const m = base.match(rule.match)
    if (m) return hinted ? `${rule.text(m)} ${t('nativeDesktop.hostError.natHint')}` : rule.text(m)
  }
  return sent
}
