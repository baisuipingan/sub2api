const IMAGE_MIME_BY_EXTENSION: Record<string, string> = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  webp: 'image/webp',
  gif: 'image/gif',
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

export function dataUrlToBytes(dataUrl: string): { ext: string; bytes: Uint8Array } {
  const match = dataUrl.match(/^data:image\/(\w+);base64,/)
  const ext = match?.[1] ?? 'png'
  const binary = atob(dataUrl.replace(/^data:[^;]+;base64,/, ''))
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return { ext, bytes }
}

export function bytesToDataUrl(bytes: Uint8Array, filePath: string): string {
  const ext = filePath.split('.').pop()?.toLowerCase() ?? 'png'
  const mime = IMAGE_MIME_BY_EXTENSION[ext] ?? 'image/png'
  return `data:${mime};base64,${bytesToBase64(bytes)}`
}

export async function blobToDataUrl(blob: Blob, fallbackMime = 'application/octet-stream'): Promise<string> {
  return `data:${blob.type || fallbackMime};base64,${bytesToBase64(new Uint8Array(await blob.arrayBuffer()))}`
}

function decodeDataUrlPayload(payload: string): Uint8Array {
  const encoded = new TextEncoder().encode(payload)
  const bytes = new Uint8Array(encoded.length)
  let offset = 0
  for (let index = 0; index < encoded.length; index += 1) {
    if (encoded[index] === 0x25) {
      const escaped = String.fromCharCode(...encoded.subarray(index + 1, index + 3))
      if (!/^[0-9a-f]{2}$/i.test(escaped)) throw new Error('Malformed data URL')
      bytes[offset++] = parseInt(escaped, 16)
      index += 2
    } else {
      bytes[offset++] = encoded[index]
    }
  }
  return bytes.subarray(0, offset)
}

/** Decode in-memory image data locally so CSP never sees a fetch(data:) call. */
export async function imageSourceToBlob(source: string, fallbackType = 'image/png'): Promise<Blob> {
  const trimmed = source.trim()
  if (!trimmed) throw new Error('Image source is not available')
  if (/^data:/i.test(trimmed)) {
    const commaIndex = trimmed.indexOf(',')
    if (commaIndex === -1) throw new Error('Malformed data URL')
    const metadata = trimmed.slice(5, commaIndex)
    const type = metadata.split(';')[0].trim() || fallbackType
    const payload = decodeDataUrlPayload(trimmed.slice(commaIndex + 1))
    if (/(?:^|;)base64(?:;|$)/i.test(metadata)) {
      const binary = atob(new TextDecoder().decode(payload))
      return new Blob([Uint8Array.from(binary, (character) => character.charCodeAt(0))], { type })
    }
    return new Blob([payload], { type })
  }
  const response = await fetch(trimmed)
  if (!response.ok) throw new Error(`Image request failed: HTTP ${response.status}`)
  const blob = await response.blob()
  return blob.type ? blob : new Blob([await blob.arrayBuffer()], { type: fallbackType })
}

export function fileToDataUrl(file: File): Promise<string> {
  return blobToDataUrl(file, file.type || 'application/octet-stream')
}
