// The labelled lines of the connection panel: what each media line has sent or
// received so far and at what rate, for the person reading it.
import { locale, t } from '../i18n'
import type { Flow, FlowKey, MicState } from './rtcFlows'
import type { RTCConnectionStats } from './rtcStats'

const SENT: FlowKey[] = ['mic', 'camera', 'screen', 'screenAudio']
const RECEIVED: FlowKey[] = ['audioIn', 'videoIn']
const LABEL: Record<FlowKey, string> = {
  mic: 'stats.flow.mic',
  camera: 'stats.flow.camera',
  screen: 'stats.flow.screen',
  screenAudio: 'stats.flow.screenAudio',
  audioIn: 'stats.flow.audioIn',
  videoIn: 'stats.flow.videoIn'
}

export function formatBytes(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  const digits = unit === 0 || value >= 100 ? 0 : 1
  return `${new Intl.NumberFormat(locale.value, { maximumFractionDigits: digits }).format(value)} ${units[unit]}`
}

function rate(kbps: number | null): string {
  if (kbps == null) return '–'
  const number = new Intl.NumberFormat(locale.value, { maximumFractionDigits: kbps >= 1000 ? 1 : 0 })
  return kbps >= 1000 ? `${number.format(kbps / 1000)} Mbit/s` : `${number.format(kbps)} kbit/s`
}

function flowValue(flow: Flow): string {
  const n = new Intl.NumberFormat(locale.value).format(flow.packets)
  return `${t('stats.flow.packets', { count: flow.packets, n })} (${rate(flow.kbps)}) · ${formatBytes(flow.bytes)}`
}

/** Why the microphone line may stand still: muted, push-to-talk idle, or a silent voice gate. */
export function micStatus(state: MicState, muted: boolean, inputMode: string): string {
  if (muted) return t('stats.mic.muted')
  if (state === 'none') return t('stats.mic.none')
  if (state === 'sending') return t('stats.mic.sending')
  return t(inputMode === 'ptt' ? 'stats.mic.pausedPtt' : 'stats.mic.pausedSilent')
}

function present(stats: RTCConnectionStats, key: FlowKey): Flow | null {
  const flow = stats.flows[key]
  // The microphone always shows; the other lines only while they carry something.
  if (key === 'mic') return flow ?? { packets: 0, bytes: 0, kbps: null, lost: 0 }
  return flow && flow.packets > 0 ? flow : null
}

/** Sent lines, then received lines, each as a label, a value and the line's name. */
export function flowRows(stats: RTCConnectionStats | null): [string, string, FlowKey][] {
  const rows: [string, string, FlowKey][] = []
  for (const key of [...SENT, ...RECEIVED]) {
    const flow = stats ? present(stats, key) : null
    if (flow) rows.push([t(LABEL[key]), flowValue(flow), key])
  }
  return rows
}
