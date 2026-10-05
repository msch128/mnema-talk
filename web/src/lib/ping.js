// Ping colour thresholds shared by the voice status panel: forest up to
// 80 ms, warning up to 200 ms, danger above. No measurement → neutral.
export function pingTone(ms) {
  if (typeof ms !== 'number' || Number.isNaN(ms)) return 'none'
  if (ms <= 80) return 'good'
  if (ms <= 200) return 'warn'
  return 'bad'
}

export const PING_CLASS = {
  good: 'text-mnema-accent',
  warn: 'text-mnema-warning',
  bad: 'text-mnema-danger',
  none: 'text-mnema-tertiary'
}
