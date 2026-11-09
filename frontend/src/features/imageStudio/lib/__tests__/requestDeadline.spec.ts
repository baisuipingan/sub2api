import { afterEach, describe, expect, it, vi } from 'vitest'
import { StudioRequestTimeoutError, withStudioRequestDeadline } from '../requestDeadline'

afterEach(() => vi.useRealTimers())

describe('Studio control request deadline', () => {
  it('times out and aborts a request even when its transport never settles', async () => {
    vi.useFakeTimers()
    let received: AbortSignal | undefined
    const request = withStudioRequestDeadline(20_000, undefined, signal => {
      received = signal
      return new Promise(() => undefined)
    })
    const failed = expect(request).rejects.toBeInstanceOf(StudioRequestTimeoutError)
    await vi.advanceTimersByTimeAsync(20_000)
    await failed
    expect(received?.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('propagates caller cancellation and clears the deadline', async () => {
    vi.useFakeTimers()
    const caller = new AbortController()
    let received: AbortSignal | undefined
    const request = withStudioRequestDeadline(120_000, caller.signal, signal => {
      received = signal
      return new Promise(() => undefined)
    })
    const failed = expect(request).rejects.toMatchObject({ name: 'AbortError' })
    caller.abort()
    await failed
    expect(received?.aborted).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps response reading inside the same deadline and clears successful timers', async () => {
    vi.useFakeTimers()
    expect(await withStudioRequestDeadline(20_000, undefined, async () => 'ready')).toBe('ready')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('cleans up when a transport throws before returning its promise', async () => {
    vi.useFakeTimers()
    await expect(withStudioRequestDeadline(20_000, undefined, () => { throw new Error('transport setup failed') })).rejects.toThrow('transport setup failed')
    expect(vi.getTimerCount()).toBe(0)
  })
})
