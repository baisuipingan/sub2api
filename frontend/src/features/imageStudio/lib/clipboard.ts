import { libT } from './i18nMessage'

export async function copyTextToClipboard(text: string) {
  let asyncClipboardError: unknown = null

  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch (err) {
      asyncClipboardError = err
    }
  }

  if (copyTextWithExecCommand(text)) return

  throw asyncClipboardError ?? new Error('Clipboard API is not available')
}

export async function copyImageSourceToClipboard(src: string | Promise<string | undefined>) {
  const resolvedSrc = await Promise.resolve(src)
  if (!resolvedSrc) throw new Error('Image source is not available')

  if (canCopyImageToClipboard()) {
    // 关键：不要用 fetch(dataUrl) 拿 blob。
    // 站点的 CSP 里 connect-src 不含 data:，浏览器会直接拒绝：
    //   Refused to connect ... violates the Content Security Policy
    // 表现就是「复制失败」，而错误根本不在剪贴板 API 上。
    // dataUrl 本来就在内存里，直接解码成二进制即可，不需要任何网络请求。
    const blob = await dataUrlToBlob(resolvedSrc)
    await writeImageBlobToClipboard(blob)
    return
  }

  // 没有现代剪贴板 API 时直接给出明确原因。
  //
  // 这里原本还试过 execCommand('copy') 兜底，实测在 Chrome 里对 <img> 恒返回 false，
  // 是个无效兜底，只会让人误以为「代码在努力但失败」。
  // 真实限制：浏览器的异步剪贴板 API 仅在安全上下文（https / localhost）可用。
  throw new Error(NEED_SECURE_CONTEXT)
}

/**
 * 把 dataUrl 解码成 Blob，不发网络请求。
 *
 * 相比 `fetch(dataUrl).then(r => r.blob())`：
 *   - 完全绕开 CSP 的 connect-src 限制
 *   - 不产生一次多余的网络栈往返
 * 非 dataUrl（http/https 地址）仍走 fetch。
 */
async function dataUrlToBlob(src: string): Promise<Blob> {
  if (!src.startsWith('data:')) {
    const res = await fetch(src)
    return res.blob()
  }
  const commaIndex = src.indexOf(',')
  if (commaIndex === -1) throw new Error('Malformed data URL')
  const meta = src.slice(5, commaIndex)
  const isBase64 = meta.includes(';base64')
  const mime = meta.split(';')[0] || 'image/png'
  const payload = src.slice(commaIndex + 1)

  if (!isBase64) {
    return new Blob([decodeURIComponent(payload)], { type: mime })
  }

  const binary = atob(payload)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return new Blob([bytes], { type: mime })
}

/**
 * 图片是否「有可能」复制。
 *
 * 注意：不要用 isSecureContext 做唯一判据 —— 用 http + IP 访问时它是 false，
 * 但 execCommand 的降级路径其实是能用的。这里只要浏览器不是彻底没有剪贴板能力就返回 true，
 * 真正失败时由调用方给提示。
 */
/**
 * 当前环境能否复制图片。
 *
 * 浏览器的异步剪贴板 API 只在「安全上下文」可用：https 或 localhost。
 * 用 http + IP 访问时（如内网测试机）无论怎么写代码都复制不了——
 * execCommand 对图片在 Chrome 里始终返回 false，不存在可用的降级路径。
 */
export function canCopyImageToClipboard() {
  if (typeof window === 'undefined') return false
  return window.isSecureContext && Boolean(navigator.clipboard?.write) && typeof ClipboardItem !== 'undefined'
}

/** 需要安全上下文才能复制时的错误标记，供调用方识别并给出针对性提示 */
export const NEED_SECURE_CONTEXT = 'NEED_SECURE_CONTEXT' as const

export function getClipboardFailureMessage(fallback: string, err: unknown) {
  // 最常见的一种：用 http + IP 访问（内网测试机）时浏览器不提供剪贴板 API。
  // 必须直接说清楚「换 https 访问」，否则用户会以为是功能坏了。
  if (err instanceof Error && err.message === NEED_SECURE_CONTEXT) {
    return libT('imageStudio.lib.clipboardNeedHttps')
  }

  if (isEmbeddedPage() && isClipboardPermissionError(err)) {
    return '复制失败：内嵌页面未授予剪贴板权限'
  }

  if (err instanceof Error && err.message.startsWith('当前浏览器不支持')) {
    return `复制失败：${err.message}`
  }

  return fallback
}

function copyTextWithExecCommand(text: string) {
  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.setAttribute('readonly', '')
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.style.top = '0'

  document.body.appendChild(textarea)
  textarea.select()
  textarea.setSelectionRange(0, textarea.value.length)

  try {
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    document.body.removeChild(textarea)
  }
}

async function writeImageBlobToClipboard(blob: Blob) {
  if (!blob.type.startsWith('image/')) throw new Error('Clipboard item is not an image')

  const clipboardItems: Record<string, Blob | Promise<Blob>> = {}
  const customType = `web ${blob.type}`

  if (isClipboardTypeSupported(customType)) {
    clipboardItems[customType] = blob
  }

  if (blob.type === 'image/png') {
    clipboardItems['image/png'] = blob
  } else if (isClipboardTypeSupported('image/png')) {
    clipboardItems['image/png'] = imageBlobToPngBlob(blob)
  }

  if (Object.keys(clipboardItems).length === 0) {
    throw new Error(libT('imageStudio.lib.clipboardUnsupported'))
  }

  await navigator.clipboard.write([
    new ClipboardItem(clipboardItems),
  ])
}

function isClipboardTypeSupported(type: string) {
  const supports = (ClipboardItem as typeof ClipboardItem & { supports?: (type: string) => boolean }).supports
  return supports ? supports(type) : type === 'image/png'
}

async function imageBlobToPngBlob(blob: Blob): Promise<Blob> {
  const image = await createImageBitmap(blob)
  try {
    const canvas = document.createElement('canvas')
    canvas.width = image.width
    canvas.height = image.height
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('Canvas is not available')
    ctx.drawImage(image, 0, 0)

    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob((pngBlob) => {
        if (pngBlob) resolve(pngBlob)
        else reject(new Error('Image conversion failed'))
      }, 'image/png')
    })
  } finally {
    image.close()
  }
}

function isEmbeddedPage() {
  try {
    return window.self !== window.top
  } catch {
    return true
  }
}

function isClipboardPermissionError(err: unknown) {
  if (!(err instanceof Error)) return false

  return (
    err.name === 'NotAllowedError' ||
    /permission|permissions policy|not allowed|denied/i.test(err.message)
  )
}
