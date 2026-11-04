// A byte count for the admin screens (storage, memory, database size): 1024 based,
// B to TB, the number in the reader's locale. Raw unit symbols on purpose: these
// are operator figures, and the symbols are the same in every language. Chat
// attachments use lib/fileSize, whose unit labels come from the messages.
const UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const

export function formatBytes(bytes: number, locale: string, maxFractionDigits = 1): string {
  if (!bytes) return '0 B'
  // Anything beyond TB stays in TB rather than reading past the unit list.
  const step = Math.min(UNITS.length - 1, Math.max(0, Math.floor(Math.log(bytes) / Math.log(1024))))
  const value = new Intl.NumberFormat(locale, { maximumFractionDigits: maxFractionDigits }).format(bytes / 1024 ** step)
  return `${value} ${UNITS[step]}`
}
