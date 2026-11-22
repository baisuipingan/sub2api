import DOMPurify from 'dompurify'
import { Unzip, UnzipInflate, zipSync } from 'fflate'

export const maxDOCXBytes = 24 * 1024 * 1024
const maxExpandedBytes = 32 * 1024 * 1024
const maxXMLBytes = 8 * 1024 * 1024
const maxMarkupCharacters = 16 * 1024 * 1024
const maxEntries = 2048
const chunkBytes = 8 * 1024
const invalidDOCX = '此文件无法作为 Word 文档预览，请下载原件查看'
const oversizedDOCX = '此 Word 文档的预览内容过大，请下载原件查看'

function zipHasEndRecord(bytes: Uint8Array) {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  for (let offset = bytes.length - 22; offset >= Math.max(0, bytes.length - 65557); offset--) {
    if (view.getUint32(offset, true) === 0x06054b50 && view.getUint16(offset + 20, true) === bytes.length - offset - 22) {
      return view.getUint16(offset + 4, true) === 0 && view.getUint16(offset + 6, true) === 0
    }
  }
  return false
}

async function boundedArchive(bytes: Uint8Array, signal: AbortSignal) {
  if (bytes.length > maxDOCXBytes) throw new Error(oversizedDOCX)
  if (!zipHasEndRecord(bytes)) throw new Error(invalidDOCX)

  const entries: Record<string, Uint8Array> = Object.create(null)
  const names = new Set<string>()
  let total = 0
  let xml = 0
  let pending = 0
  const unzip = new Unzip(file => {
    const name = file.name
    const directory = name.endsWith('/')
    const segments = (directory ? name.slice(0, -1) : name).split('/')
    if (names.has(name) || name.includes('\\') || segments.some(part => !part || ['.', '..', '__proto__', 'constructor', 'prototype'].includes(part))) throw new Error(invalidDOCX)
    names.add(name)
    if (names.size > maxEntries || (file.originalSize || 0) > maxExpandedBytes - total) throw new Error(oversizedDOCX)
    const parts: Uint8Array[] = []
    const isXML = /\.(?:xml|rels)$/i.test(name)
    let length = 0
    pending++
    file.ondata = (err, data, final) => {
      if (err) throw err
      signal.throwIfAborted()
      total += data.length
      length += data.length
      if (isXML) xml += data.length
      if (total > maxExpandedBytes || xml > maxXMLBytes) throw new Error(oversizedDOCX)
      if (data.length > 0) parts.push(data)
      if (!final) return
      pending--
      if (directory) {
        if (length) throw new Error(invalidDOCX)
        return
      }
      if (file.originalSize !== undefined && file.originalSize !== length) throw new Error(invalidDOCX)
      const content = new Uint8Array(length)
      let offset = 0
      for (const part of parts) {
        content.set(part, offset)
        offset += part.length
      }
      entries[name] = content
    }
    file.start()
  })
  unzip.register(UnzipInflate)

  let lastYieldSize = 0
  for (let offset = 0; offset < bytes.length; offset += chunkBytes) {
    signal.throwIfAborted()
    const end = Math.min(offset + chunkBytes, bytes.length)
    unzip.push(bytes.subarray(offset, end), end === bytes.length)
    if (end < bytes.length && (end % (chunkBytes * 32) === 0 || total - lastYieldSize >= 2 * 1024 * 1024)) {
      lastYieldSize = total
      await new Promise(resolve => setTimeout(resolve, 0))
    }
  }
  if (pending || !entries['[Content_Types].xml'] || !entries['_rels/.rels'] || !entries['word/document.xml']) throw new Error(invalidDOCX)
  // Give the renderer only verified entries, independent of the input ZIP index.
  return zipSync(entries, { level: 0 })
}

const csp = "default-src 'none'; style-src 'unsafe-inline'; img-src data:"
const layout = `
  html { color-scheme: light; background: #eef1f5; }
  body { margin: 0; }
  .docx-wrapper { padding: 20px 12px !important; background: transparent !important; }
  .docx-wrapper > section.docx {
    width: 100%;
    max-width: 816px;
    min-width: 0;
    box-sizing: border-box;
    padding: clamp(24px, 5vw, 64px);
    overflow-wrap: anywhere;
    box-shadow: 0 1px 6px rgb(15 23 42 / 14%);
  }
  .docx-preview-table { max-width: 100%; overflow-x: auto; }
  .docx-wrapper img { max-width: 100%; object-fit: contain; }
  @media (max-width: 550px) {
    .docx-wrapper { padding: 10px 8px !important; }
    .docx-wrapper > section.docx { padding: clamp(18px, 5vw, 28px) !important; }
  }
`

export async function renderDOCXPreview(bytes: Uint8Array, signal: AbortSignal) {
  try {
    const archive = await boundedArchive(bytes, signal)
    const { renderAsync } = await import('docx-preview')
    signal.throwIfAborted()
    const content = document.createElement('div')
    const styles = document.createElement('div')
    await renderAsync(archive, content, styles, {
      ignoreWidth: true,
      ignoreHeight: true,
      ignoreFonts: true,
      renderAltChunks: false,
      useBase64URL: true,
    })
    signal.throwIfAborted()

    for (const image of content.querySelectorAll('img')) {
      if (!/^data:image\/(?:png|jpeg|gif|webp|bmp);base64,/i.test(image.getAttribute('src') || '')) image.remove()
    }
    for (const table of content.querySelectorAll('.docx > :is(article, header, footer) > table')) {
      const scrollArea = document.createElement('div')
      scrollArea.className = 'docx-preview-table'
      table.replaceWith(scrollArea)
      scrollArea.append(table)
    }
    const markup = DOMPurify.sanitize(content, {
      ALLOWED_TAGS: ['div', 'section', 'article', 'header', 'footer', 'p', 'span', 'br', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'table', 'tbody', 'thead', 'tfoot', 'tr', 'td', 'th', 'colgroup', 'col', 'ol', 'ul', 'li', 'img', 'sup', 'sub'],
      ALLOWED_ATTR: ['class', 'style', 'alt', 'src', 'colspan', 'rowspan', 'span', 'width', 'height', 'dir', 'lang', 'start'],
      ALLOW_DATA_ATTR: false,
      ALLOW_ARIA_ATTR: false,
    })
    const css = Array.from(styles.querySelectorAll('style'), element => element.textContent).join('\n').replace(/</g, '\\3c ')
    if (markup.length + css.length > maxMarkupCharacters) throw new Error(oversizedDOCX)
    return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="${csp}"><style>${css}\n${layout}</style></head><body>${markup}</body></html>`
  } catch (error) {
    signal.throwIfAborted()
    if (error instanceof Error && error.message === oversizedDOCX) throw error
    throw new Error(invalidDOCX)
  }
}
