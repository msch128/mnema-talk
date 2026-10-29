// Text of the admin System tab's built-in TURN relay line.
import { t } from '../i18n'
import type { ServerBuiltinTURNHealth } from '../types/rest'

// What the built-in TURN relay is doing, in one line: off, not running
// although asked for, or listening (with the self-test result as a hint).
export function builtinTurnText(b: ServerBuiltinTURNHealth | undefined): string {
  if (!b) return ''
  if (!b.active) return b.enabled ? t('admin.system.builtinTurnDown') : t('admin.system.builtinTurnOff')
  const transports = [b.udp ? 'UDP' : '', b.tcp ? 'TCP' : ''].filter(Boolean).join('+')
  return t('admin.system.builtinTurnActive', { port: b.port, transports, count: b.allocations, reach: t(`admin.system.turnReach.${b.reachability}`) })
}
