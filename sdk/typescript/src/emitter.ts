// A small typed event emitter. A listener that throws or returns a rejected
// promise does not break the emitter: the error goes to onListenerError.

type EventMap = { [event: string]: unknown[] }
type Entry = { fn: (...args: never[]) => unknown; once: boolean }

export class Emitter<E extends { [K in keyof E]: unknown[] } = EventMap> {
  private readonly listeners = new Map<keyof E, Entry[]>()

  on<K extends keyof E>(event: K, listener: (...args: E[K]) => unknown): this {
    return this.add(event, listener, false)
  }

  once<K extends keyof E>(event: K, listener: (...args: E[K]) => unknown): this {
    return this.add(event, listener, true)
  }

  off<K extends keyof E>(event: K, listener: (...args: E[K]) => unknown): this {
    const list = this.listeners.get(event)
    if (list) this.listeners.set(event, list.filter((e) => e.fn !== listener))
    return this
  }

  listenerCount<K extends keyof E>(event: K): number {
    return this.listeners.get(event)?.length ?? 0
  }

  /** Calls the listeners of `event`; answers whether there were any. */
  protected emit<K extends keyof E>(event: K, ...args: E[K]): boolean {
    const list = this.listeners.get(event)
    if (!list || list.length === 0) return false
    this.listeners.set(event, list.filter((e) => !e.once))
    for (const e of list) {
      try {
        const out = (e.fn as (...a: E[K]) => unknown)(...args)
        if (out instanceof Promise) out.catch((err: unknown) => this.onListenerError(err, String(event)))
      } catch (err) {
        this.onListenerError(err, String(event))
      }
    }
    return true
  }

  /** Where errors of listeners go; subclasses route them to their 'error' event. */
  protected onListenerError(err: unknown, event: string): void {
    console.error(`mnema-talk-sdk: a listener of "${event}" failed:`, err)
  }

  private add<K extends keyof E>(event: K, fn: (...args: E[K]) => unknown, once: boolean): this {
    const list = this.listeners.get(event) ?? []
    list.push({ fn: fn as Entry['fn'], once })
    this.listeners.set(event, list)
    return this
  }
}

/** Turns anything thrown into an Error. */
export function toError(err: unknown): Error {
  return err instanceof Error ? err : new Error(String(err))
}
