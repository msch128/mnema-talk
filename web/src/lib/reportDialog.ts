// The "Report message" dialog. A single <ReportDialog> host in App.vue shows
// whatever is pending, like lib/editHistory.
import { shallowRef } from 'vue'

export interface ReportRequest {
  messageId: string
  /** Whose message it is, for the dialog's sentence. */
  author: string
}

/** The reasons the server knows, in the order they are offered. */
export const REPORT_CATEGORIES = ['spam', 'harassment', 'illegal', 'other'] as const
export type ReportCategory = typeof REPORT_CATEGORIES[number]
export const REPORT_NOTE_MAX = 500

export const pendingReport = shallowRef<ReportRequest | null>(null)

export function openReport(request: ReportRequest) {
  pendingReport.value = request
}

export function closeReport() {
  pendingReport.value = null
}
