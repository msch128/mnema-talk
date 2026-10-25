import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastType = 'info' | 'success' | 'error'
export interface ToastAction { label: string; onClick: () => void | Promise<void> }
export interface ToastOptions { detail?: string; action?: ToastAction | null; duration?: number }
export interface ToastInput extends ToastOptions { type?: ToastType; text?: string }
export interface Toast { id: number; type: ToastType; text: string; detail: string; action: ToastAction | null }

const AUTO_DISMISS_MS = 5000
const MAX_VISIBLE = 4
const MIN_RESUME_MS = 1000

// Toasts: info / success / error. They dismiss themselves after 5 s, except
// errors and toasts with an action, which stay until closed. Hovering or
// focusing a toast pauses its timer, so there is time to read it.
export const useToastStore = defineStore('toast', () => {
  const toasts = ref<Toast[]>([])
  const timers = new Map<number, ReturnType<typeof setTimeout>>()
  // The time each running timer still has left, kept while it is paused.
  const left = new Map<number, { ms: number; since: number }>()
  let seq = 0

  function dismiss(id: number) {
    clearTimeout(timers.get(id))
    timers.delete(id)
    left.delete(id)
    toasts.value = toasts.value.filter(t => t.id !== id)
  }

  function arm(id: number, ms: number) {
    left.set(id, { ms, since: Date.now() })
    timers.set(id, setTimeout(() => dismiss(id), ms))
  }

  /** Stops the countdown while the pointer or focus is on the toast. */
  function pause(id: number) {
    const entry = left.get(id)
    if (!entry || !timers.has(id)) return
    clearTimeout(timers.get(id))
    timers.delete(id)
    left.set(id, { ms: Math.max(entry.ms - (Date.now() - entry.since), 0), since: entry.since })
  }

  /** Continues a paused countdown (with at least a second to spare). */
  function resume(id: number) {
    const entry = left.get(id)
    if (!entry || timers.has(id)) return
    arm(id, Math.max(entry.ms, MIN_RESUME_MS))
  }

  /** push({ type, text, detail, action: { label, onClick }, duration }) → id */
  function push({ type = 'info', text, detail = '', action = null, duration }: ToastInput = {}) {
    if (!text) return null
    const id = ++seq
    toasts.value = [...toasts.value, { id, type, text, detail, action }].slice(-MAX_VISIBLE)
    const sticky = type === 'error' || !!action
    const ms = duration ?? (sticky ? 0 : AUTO_DISMISS_MS)
    if (ms > 0) arm(id, ms)
    // Drop timers of toasts that fell off the stack.
    for (const tid of [...timers.keys()]) {
      if (!toasts.value.some(t => t.id === tid)) { clearTimeout(timers.get(tid)); timers.delete(tid); left.delete(tid) }
    }
    return id
  }

  const info = (text: string, o?: ToastOptions) => push({ ...o, type: 'info', text })
  const success = (text: string, o?: ToastOptions) => push({ ...o, type: 'success', text })
  const error = (text: string, o?: ToastOptions) => push({ ...o, type: 'error', text })

  return { toasts, push, dismiss, pause, resume, info, success, error }
})
