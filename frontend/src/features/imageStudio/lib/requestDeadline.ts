export class StudioRequestTimeoutError extends Error {
  constructor() { super('Studio request timed out'); this.name = 'TimeoutError' }
}

/** Bound the complete control request, including reading its response body.
 * Cancellation settles even if a custom transport ignores AbortSignal. */
export function withStudioRequestDeadline<T>(timeoutMs: number, signal: AbortSignal | null | undefined, execute: (signal: AbortSignal) => Promise<T>): Promise<T> {
  const controller = new AbortController()
  return new Promise<T>((resolve, reject) => {
    let settled = false
    const finish = (callback: () => void) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      signal?.removeEventListener('abort', abort)
      callback()
    }
    const abort = () => {
      controller.abort()
      finish(() => reject(new DOMException('Studio request cancelled', 'AbortError')))
    }
    const timer = setTimeout(() => {
      controller.abort()
      finish(() => reject(new StudioRequestTimeoutError()))
    }, timeoutMs)
    signal?.addEventListener('abort', abort, { once: true })
    if (signal?.aborted) { abort(); return }
    try {
      void execute(controller.signal).then(value => finish(() => resolve(value)), error => finish(() => reject(error)))
    } catch (error) { finish(() => reject(error)) }
  })
}
