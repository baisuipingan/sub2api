export const TICKET_MAX_FILES = 3
export const TICKET_SOURCE_MAX_BYTES = 8 * 1024 * 1024
export const TICKET_UPLOAD_MAX_BYTES = 2 * 1024 * 1024
const MAX_PIXELS = 12_000_000
const MAX_EDGE = 2560

export class TicketImageError extends Error {
  constructor(public readonly reason: 'count' | 'size' | 'format' | 'pixels' | 'compress') { super(reason) }
}

/** Inspect dimensions before allocating decoded pixels. The server checks again. */
export function inspectTicketImage(bytes: Uint8Array): { width: number; height: number; mime: string } {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const text = (offset: number, count: number) => String.fromCharCode(...bytes.subarray(offset, offset + count))
  let width = 0; let height = 0; let mime = ''
  if (bytes.length >= 33 && bytes[0] === 137 && text(1, 3) === 'PNG' && view.getUint32(4) === 0x0d0a1a0a) {
    if (text(12, 4) !== 'IHDR') throw new TicketImageError('format')
    width = view.getUint32(16); height = view.getUint32(20); mime = 'image/png'
    for (let offset = 8; offset + 12 <= bytes.length;) {
      const length = view.getUint32(offset)
      if (length > bytes.length - offset - 12) throw new TicketImageError('format')
      if (text(offset + 4, 4) === 'acTL') throw new TicketImageError('format')
      offset += length + 12
    }
  } else if (bytes.length >= 12 && text(0, 4) === 'RIFF' && text(8, 4) === 'WEBP') {
    mime = 'image/webp'
    for (let offset = 12; offset + 8 <= bytes.length;) {
      const kind = text(offset, 4); const length = view.getUint32(offset + 4, true); const data = offset + 8
      if (length > bytes.length - data) throw new TicketImageError('format')
      if (kind === 'ANIM' || kind === 'ANMF') throw new TicketImageError('format')
      if (kind === 'VP8X' && length >= 10) {
        if (bytes[data]! & 2) throw new TicketImageError('format')
        width = 1 + bytes[data + 4]! + (bytes[data + 5]! << 8) + (bytes[data + 6]! << 16)
        height = 1 + bytes[data + 7]! + (bytes[data + 8]! << 8) + (bytes[data + 9]! << 16)
      } else if (kind === 'VP8 ' && length >= 10 && !width) {
        if (text(data + 3, 3) !== '\x9d\x01\x2a') throw new TicketImageError('format')
        width = view.getUint16(data + 6, true) & 0x3fff; height = view.getUint16(data + 8, true) & 0x3fff
      } else if (kind === 'VP8L' && length >= 5 && !width) {
        if (bytes[data] !== 0x2f) throw new TicketImageError('format')
        const packed = view.getUint32(data + 1, true)
        width = (packed & 0x3fff) + 1; height = ((packed >>> 14) & 0x3fff) + 1
      }
      offset = data + length + (length % 2)
    }
  } else if (bytes.length >= 4 && bytes[0] === 255 && bytes[1] === 216) {
    mime = 'image/jpeg'
    for (let offset = 2; offset + 4 <= bytes.length;) {
      if (bytes[offset] !== 255) throw new TicketImageError('format')
      while (bytes[offset] === 255) offset++
      const marker = bytes[offset++]!
      if (marker === 0xd9 || marker === 0xda) break
      if (marker === 1 || (marker >= 0xd0 && marker <= 0xd7)) continue
      if (offset + 2 > bytes.length) break
      const length = view.getUint16(offset)
      if (length < 2 || offset + length > bytes.length) throw new TicketImageError('format')
      if ([0xc0, 0xc1, 0xc2].includes(marker) && length >= 8) {
        height = view.getUint16(offset + 3); width = view.getUint16(offset + 5); break
      }
      offset += length
    }
  }
  if (!width || !height || !mime) throw new TicketImageError('format')
  if (width * height > MAX_PIXELS) throw new TicketImageError('pixels')
  return { width, height, mime }
}

function blobBytes(blob: Blob): Promise<ArrayBuffer> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.onerror = () => reject(new TicketImageError('format'))
    reader.readAsArrayBuffer(blob)
  })
}
function encode(canvas: HTMLCanvasElement, mime: string, quality?: number): Promise<Blob | null> {
  return new Promise(resolve => canvas.toBlob(resolve, mime, quality))
}

export async function prepareTicketImage(file: File): Promise<File> {
  if (!file.size || file.size > TICKET_SOURCE_MAX_BYTES) throw new TicketImageError('size')
  const dimensions = inspectTicketImage(new Uint8Array(await blobBytes(file)))
  const sourceURL = URL.createObjectURL(file)
  const image = new Image()
  try {
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new TicketImageError('format'))
      image.src = sourceURL
    })
    if (image.naturalWidth !== dimensions.width || image.naturalHeight !== dimensions.height) throw new TicketImageError('format')
    const canvas = document.createElement('canvas')
    let scale = Math.min(1, MAX_EDGE / Math.max(dimensions.width, dimensions.height))
    for (let attempt = 0; attempt < 4; attempt++) {
      canvas.width = Math.max(1, Math.round(dimensions.width * scale))
      canvas.height = Math.max(1, Math.round(dimensions.height * scale))
      const context = canvas.getContext('2d')
      if (!context) throw new TicketImageError('compress')
      context.drawImage(image, 0, 0, canvas.width, canvas.height)
      // Lossless PNG keeps interface text crisp when it fits. High-quality
      // WebP is a fallback for photographic or particularly large screenshots.
      const png = await encode(canvas, 'image/png')
      if (png && png.size <= TICKET_UPLOAD_MAX_BYTES) return new File([png], 'screenshot.png', { type: png.type })
      for (const quality of [0.94, 0.86]) {
        const webp = await encode(canvas, 'image/webp', quality)
        if (webp && webp.size <= TICKET_UPLOAD_MAX_BYTES) {
          return new File([webp], webp.type === 'image/webp' ? 'screenshot.webp' : 'screenshot.png', { type: webp.type })
        }
      }
      scale *= 0.8
    }
    throw new TicketImageError('compress')
  } finally { URL.revokeObjectURL(sourceURL) }
}

/** Reuse one operation key after an uncertain response, until the draft changes. */
export function createTicketOperation() {
  let signature = ''; let key = ''; let fileSequence = 0
  const identities = new WeakMap<File, number>()
  return {
    keyFor(fields: Record<string, string | number>, images: readonly File[]) {
      const files = images.map(file => {
        if (!identities.has(file)) identities.set(file, ++fileSequence)
        return identities.get(file)
      })
      const next = JSON.stringify([fields, files])
      if (!key || next !== signature) { signature = next; key = crypto.randomUUID() }
      return key
    },
    reset() { signature = ''; key = '' }
  }
}
