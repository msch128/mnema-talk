import { defineStore } from 'pinia'
import { ref } from 'vue'

export type ToastType = 'info' | 'success' | 'error'
export interface ToastAction { label: string; onClick: () => void | Promise<void> }
export interface ToastOptions { detail?: string; action?: ToastAction | null; duration?: number }
export interface ToastInput extends ToastOptions { type?: ToastType; text?: string }
export interface Toast { id: number; type: ToastType; text: string; detail: string; action: ToastAction | null }

const AUTO_DISMISS_MS = 5000
const MAX_VISIBLE = 4

// Toasts: info / success / error. They dismiss themselves after 5 s, except
// errors and toasts with an action, which stay until closed.
export const useToastStore = defineStore('toast', () => {
  const toasts = ref<Toast[]>([])
  const timers = new Map<number, ReturnType<typeof setTimeout>>()
  let seq = 0

  function dismiss(id: number) {
    clearTimeout(timers.get(id))
    timers.delete(id)
    toasts.value = toasts.value.filter(t => t.id !== id)
  }

  /** push({ type, text, detail, action: { label, onClick }, duration }) → id */
  function push({ type = 'info', text, detail = '', action = null, duration }: ToastInput = {}) {
    if (!text) return null
    const id = ++seq
    toasts.value = [...toasts.value, { id, type, text, detail, action }].slice(-MAX_VISIBLE)
    const sticky = type === 'error' || !!action
    const ms = duration ?? (sticky ? 0 : AUTO_DISMISS_MS)
    if (ms > 0) timers.set(id, setTimeout(() => dismiss(id), ms))
    // Drop timers of toasts that fell off the stack.
    for (const tid of [...timers.keys()]) {
      if (!toasts.value.some(t => t.id === tid)) { clearTimeout(timers.get(tid)); timers.delete(tid) }
    }
    return id
  }

  const info = (text: string, o?: ToastOptions) => push({ ...o, type: 'info', text })
  const success = (text: string, o?: ToastOptions) => push({ ...o, type: 'success', text })
  const error = (text: string, o?: ToastOptions) => push({ ...o, type: 'error', text })

  return { toasts, push, dismiss, info, success, error }
})
