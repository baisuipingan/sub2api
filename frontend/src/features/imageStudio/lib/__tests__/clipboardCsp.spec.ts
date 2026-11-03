import { afterEach, describe, expect, it, vi } from 'vitest'

import { copyImageSourceToClipboard } from '../clipboard'

/**
 * 回归测试：复制图片不能依赖 fetch(dataUrl)。
 *
 * 线上真实故障：站点 CSP 的 connect-src 不含 data:，浏览器直接拒绝
 *   Refused to connect to 'data:image/png;base64,...' because it violates
 *   the document's Content Security Policy.
 * 旧实现用 fetch(dataUrl) 拿 blob，于是「复制图片」必然失败——
 * 报错完全不在剪贴板 API 上，排查方向容易被带偏。
 *
 * 正确做法：dataUrl 本来就在内存里，直接解码成 Blob，不发任何请求。
 */

const PNG_1PX =
  'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('copyImageSourceToClipboard', () => {
  it('复制 dataUrl 时不应发起任何 fetch（CSP 会拦 connect-src data:）', async () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)

    const written: Blob[] = []
    vi.stubGlobal('navigator', {
      clipboard: {
        write: async (items: ClipboardItem[]) => {
          const item = items[0]
          const blob = item.getType('image/png')
          written.push(blob)
        },
      },
    })
    vi.stubGlobal(
      'ClipboardItem',
      class {
        private types: Record<string, Blob>
        constructor(types: Record<string, Blob>) {
          this.types = types
        }
        getType(type: string) {
          return this.types[type] ?? this.types['web image/png']
        }
      },
    )
    vi.stubGlobal('ClipboardItemSupports', undefined)
    vi.stubGlobal('window', { isSecureContext: true, navigator: globalThis.navigator })

    await copyImageSourceToClipboard(PNG_1PX)

    // 核心断言：一次 fetch 都不该有
    expect(fetchSpy).not.toHaveBeenCalled()
    expect(written.length).toBe(1)
    expect(written[0].type).toBe('image/png')
  })

  it('dataUrl 应被正确解码成二进制（不经过网络）', async () => {
    const fetchSpy = vi.fn()
    vi.stubGlobal('fetch', fetchSpy)

    let captured: Blob | null = null
    vi.stubGlobal('navigator', {
      clipboard: {
        write: async (items: ClipboardItem[]) => {
          captured = items[0].getType('image/png')
        },
      },
    })
    vi.stubGlobal(
      'ClipboardItem',
      class {
        private types: Record<string, Blob>
        constructor(types: Record<string, Blob>) {
          this.types = types
        }
        getType(type: string) {
          return this.types[type] ?? this.types['web image/png']
        }
      },
    )
    vi.stubGlobal('window', { isSecureContext: true, navigator: globalThis.navigator })

    await copyImageSourceToClipboard(PNG_1PX)

    expect(fetchSpy).not.toHaveBeenCalled()
    expect(captured).not.toBeNull()
    // 1x1 PNG 解码后是 67 字节左右，只要不是 0 就说明真的解出来了
    expect((captured as unknown as Blob).size).toBeGreaterThan(0)
  })

  it('非 dataUrl（真实 http 地址）仍可走 fetch', async () => {
    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: 'image/png' })
    const fetchSpy = vi.fn(async () => ({ blob: async () => blob }))
    vi.stubGlobal('fetch', fetchSpy)
    vi.stubGlobal('navigator', { clipboard: { write: async () => undefined } })
    vi.stubGlobal(
      'ClipboardItem',
      class {
        constructor(_types: Record<string, Blob>) {}
      },
    )
    vi.stubGlobal('window', { isSecureContext: true, navigator: globalThis.navigator })

    await copyImageSourceToClipboard('https://example.com/a.png')
    expect(fetchSpy).toHaveBeenCalledTimes(1)
  })
})
