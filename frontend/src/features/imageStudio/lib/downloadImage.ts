let lastDownloadTimestamp = 0

export async function downloadImageSource(
  source: string | Promise<string | undefined>,
  filename = createImageDownloadFileName(),
) {
  const resolvedSource = await Promise.resolve(source)
  if (!resolvedSource) throw new Error('Image source is not available')

  const response = await fetch(resolvedSource)
  if (!response.ok) throw new Error('Image download failed')

  const blob = await response.blob()
  const objectUrl = URL.createObjectURL(blob)
  const link = document.createElement('a')
  const extension = getImageExtension(blob.type)
  link.href = objectUrl
  link.download = `${filename}.${extension}`
  link.rel = 'noopener'
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0)
}

export function createImageDownloadFileName(now = Date.now()) {
  const timestamp = Math.max(now, lastDownloadTimestamp + 1)
  lastDownloadTimestamp = timestamp
  const date = new Date(timestamp)
  const pad = (value: number, length = 2) => String(value).padStart(length, '0')
  return `milorouter-image-${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}_${pad(date.getHours())}-${pad(date.getMinutes())}-${pad(date.getSeconds())}-${pad(date.getMilliseconds(), 3)}`
}

function getImageExtension(mimeType: string) {
  if (mimeType === 'image/jpeg') return 'jpg'
  if (mimeType === 'image/webp') return 'webp'
  if (mimeType === 'image/gif') return 'gif'
  if (mimeType === 'image/avif') return 'avif'
  return 'png'
}
