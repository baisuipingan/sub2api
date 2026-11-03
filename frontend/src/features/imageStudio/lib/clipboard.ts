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

  // 降级：非安全上下文（如用 IP + http 访问测试机）没有 navigator.clipboard.write，
  // 此时退回「选中图片 + execCommand('copy')」这条老路子。
  // 之前这里直接抛错，菜单项被隐藏，用户根本看不到「复制图片」。
  if (copyImageWithExecCommand(resolvedSrc)) return

  throw new Error('Clipboard image API is not available')
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
export function canCopyImageToClipboard() {
  if (typeof window === 'undefined') return false
  const hasModernApi =
    window.isSecureContext && Boolean(navigator.clipboard?.write) && typeof ClipboardItem !== 'undefined'
  // document.execCommand 在所有主流浏览器都存在（虽然已废弃），作为降级手段
  const hasLegacyApi = typeof document !== 'undefined' && typeof document.execCommand === 'function'
  return hasModernApi || hasLegacyApi
}

/**
 * 用 execCommand 复制图片。
 *
 * 原理：把图片放进一个 contenteditable 容器并全选，再执行 copy。
 * 部分浏览器会把它作为图片写入剪贴板；不支持时会失败，由调用方提示。
 */
function copyImageWithExecCommand(src: string): boolean {
  if (typeof document === 'undefined') return false
  const container = document.createElement('div')
  container.contentEditable = 'true'
  container.style.position = 'fixed'
  container.style.left = '-9999px'
  container.style.top = '0'
  container.style.opacity = '0'

  const img = document.createElement('img')
  img.src = src
  container.appendChild(img)
  document.body.appendChild(container)

  const selection = window.getSelection()
  try {
    const range = document.createRange()
    range.selectNodeContents(container)
    selection?.removeAllRanges()
    selection?.addRange(range)
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    selection?.removeAllRanges()
    container.remove()
  }
}

export function getClipboardFailureMessage(fallback: string, err: unknown) {
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
