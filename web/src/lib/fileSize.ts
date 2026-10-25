// A file size in the unit that fits: bytes, KB, MB or GB (1024 based, one
// decimal at most). The unit label comes from the media.size* messages.
import { t } from '../i18n'

const KB = 1024
const MB = KB * 1024
const GB = MB * 1024

export function formatFileSize(bytes: number | null | undefined, locale: string): string {
  const n = Math.max(0, bytes || 0)
  const number = (v: number) => new Intl.NumberFormat([locale], { maximumFractionDigits: 1 }).format(v)
  if (n < KB) return t('media.sizeB', { size: number(n) })
  if (n < MB) return t('media.sizeKb', { size: number(n / KB) })
  if (n < GB) return t('media.sizeMb', { size: number(n / MB) })
  return t('media.sizeGb', { size: number(n / GB) })
}
