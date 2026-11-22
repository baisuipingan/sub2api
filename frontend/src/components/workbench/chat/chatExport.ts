import { zipSync, strToU8 } from 'fflate'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { saveAs } from 'file-saver'
import { chatArtifacts, chatText } from '@/types/workbenchChat'
import type { ChatMessage } from '@/types/workbenchChat'

export type ChatExportFormat = 'docx' | 'pdf' | 'md' | 'txt'
export interface ChatExportInput { messages: readonly ChatMessage[]; title?: string }
export const chatExportFormats: ReadonlyArray<{ value: ChatExportFormat; label: string; color: string; hint?: string }> = [
  { value: 'docx', label: '导出为 Word', color: '#4176e6' },
  { value: 'pdf', label: '导出为 PDF', color: '#e84b3c', hint: '打开系统打印窗口，选择“另存为 PDF”' },
  { value: 'md', label: '导出为 Markdown', color: '#f58a1f' },
  { value: 'txt', label: '导出为 TXT', color: '#5c6680' },
]

/** Only the messages explicitly supplied by the active view may leave the page. */
export function exportableChatMessages(messages: readonly ChatMessage[]): ChatMessage[] {
  const seen = new Set<string>()
  return messages.filter(message => {
    if (!['user', 'assistant'].includes(message.role) || seen.has(message.id)) return false
    seen.add(message.id)
    return true
  })
}

function exportTitle(input: ChatExportInput) { return input.title?.trim().replace(/[\r\n]+/g, ' ') || '对话记录' }
function messageText(message: ChatMessage) { const text = chatText(message.content); return typeof text === 'string' ? text : '' }
function fileNames(message: ChatMessage) { return chatArtifacts(message.content).map(file => file.name).filter(name => typeof name === 'string') }
function roleName(message: ChatMessage) { return message.role === 'user' ? '你' : '助手' }
function selected(input: ChatExportInput) { const messages = exportableChatMessages(input.messages); if (!messages.length) throw new Error('请先选择要导出的消息'); return messages }
export function chatExportFilename(title: string | undefined, extension: ChatExportFormat): string {
  let name = Array.from(title?.trim() || '对话记录').map(character => character.codePointAt(0)! < 32 ? '_' : character).join('').replace(/[\\/:*?"<>|]/g, '_').replace(/[. ]+$/g, '').slice(0, 100)
  if (!name || /^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i.test(name)) name = `对话记录${name ? `_${name}` : ''}`
  return `${name}.${extension}`
}

export function buildChatMarkdown(input: ChatExportInput): string {
  const body = selected(input).map(message => {
    const files = fileNames(message)
    return `## ${roleName(message)}\n\n${messageText(message)}${files.length ? `\n\n文件：${files.join('、')}` : ''}`
  }).join('\n\n---\n\n')
  return `# ${exportTitle(input)}\n\n${body}\n`
}

const allowedTags = ['p', 'br', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'strong', 'b', 'em', 'i', 'del', 's', 'code', 'pre', 'blockquote', 'ul', 'ol', 'li', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'hr', 'a', 'span']
function escapeHtml(value: string) { return value.replace(/[&<>"']/g, character => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[character]!)) }

/** A detached, sanitised tree with no resource attributes, embedded media or remote fetches. */
function messageFragment(message: ChatMessage): DocumentFragment {
  const renderer = new marked.Renderer()
  renderer.image = ({ text }) => escapeHtml(`[图片${text ? `：${text}` : ''}]`)
  const rendered = marked.parse(messageText(message), { renderer, gfm: true, breaks: false, async: false }) as string
  const fragment = DOMPurify.sanitize(rendered, { ALLOWED_TAGS: allowedTags, ALLOWED_ATTR: [], ALLOW_DATA_ATTR: false, RETURN_DOM_FRAGMENT: true })
  const names = fileNames(message)
  if (names.length) { const paragraph = document.createElement('p'); paragraph.textContent = `文件：${names.join('、')}`; fragment.append(paragraph) }
  return fragment
}

function plainNode(node: Node, depth = 0): string {
  if (node.nodeType === Node.TEXT_NODE) return node.textContent || ''
  if (!(node instanceof HTMLElement)) return Array.from(node.childNodes).map(child => plainNode(child, depth)).join('')
  if (node.tagName === 'BR') return '\n'
  if (node.tagName === 'PRE') return `${node.textContent || ''}\n\n`
  if (node.tagName === 'UL' || node.tagName === 'OL') {
    return Array.from(node.children).filter(child => child.tagName === 'LI').map((child, index) => `${'  '.repeat(depth)}${node.tagName === 'OL' ? `${index + 1}.` : '-'} ${Array.from(child.childNodes).map(part => plainNode(part, depth + 1)).join('').trim()}\n`).join('') + '\n'
  }
  if (node.tagName === 'TABLE') return Array.from(node.querySelectorAll('tr')).map(row => Array.from(row.children).map(cell => cell.textContent || '').join('\t')).join('\n') + '\n\n'
  if (node.tagName === 'HR') return '────────────────\n\n'
  const content = Array.from(node.childNodes).map(child => plainNode(child, depth)).join('')
  return /^(P|H[1-6]|BLOCKQUOTE)$/.test(node.tagName) ? `${content.trim()}\n\n` : content
}

export function buildChatText(input: ChatExportInput): string {
  return `${exportTitle(input)}\n\n` + selected(input).map(message => `${roleName(message)}\n\n${plainNode(messageFragment(message)).trim()}`).join('\n\n────────────────\n\n') + '\n'
}

/** XML 1.0 excludes control characters and unpaired UTF-16 surrogates. */
function xml(value: string): string {
  const valid = Array.from(value).filter(character => { const code = character.codePointAt(0)!; return (code >= 32 || [9, 10, 13].includes(code)) && code !== 0xfffe && code !== 0xffff }).map(character => { const code = character.codePointAt(0)!; return code >= 0xd800 && code <= 0xdfff ? '\ufffd' : character }).join('')
  return valid.replace(/[&<>"']/g, character => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&apos;' }[character]!))
}
interface RunStyle { bold?: boolean; italic?: boolean; strike?: boolean; code?: boolean }
interface WordContext { nextNumberId: number; lists: Array<{ id: number; ordered: boolean }> }
function wordRun(text: string, style: RunStyle = {}): string {
  const properties = `${style.bold ? '<w:b/>' : ''}${style.italic ? '<w:i/>' : ''}${style.strike ? '<w:strike/>' : ''}${style.code ? '<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New" w:eastAsia="Microsoft YaHei"/><w:sz w:val="20"/>' : ''}`
  const content = text.split('\n').map((line, index) => `${index ? '<w:br/>' : ''}<w:t xml:space="preserve">${xml(line)}</w:t>`).join('')
  return `<w:r>${properties ? `<w:rPr>${properties}</w:rPr>` : ''}${content}</w:r>`
}
function inlineWord(node: Node, style: RunStyle = {}): string {
  if (node.nodeType === Node.TEXT_NODE) return wordRun(node.textContent || '', style)
  if (!(node instanceof HTMLElement)) return Array.from(node.childNodes).map(child => inlineWord(child, style)).join('')
  if (node.tagName === 'BR') return '<w:r><w:br/></w:r>'
  const next = { ...style, ...(/^(STRONG|B)$/.test(node.tagName) ? { bold: true } : {}), ...(/^(EM|I)$/.test(node.tagName) ? { italic: true } : {}), ...(/^(DEL|S)$/.test(node.tagName) ? { strike: true } : {}), ...(node.tagName === 'CODE' ? { code: true } : {}) }
  return Array.from(node.childNodes).map(child => inlineWord(child, next)).join('')
}
function paragraph(content: string, style = 'Normal', extra = '') { return `<w:p><w:pPr><w:pStyle w:val="${style}"/>${extra}</w:pPr>${content || '<w:r><w:t xml:space="preserve"> </w:t></w:r>'}</w:p>` }
function wordList(list: HTMLElement, depth: number, context: WordContext): string {
  const numberId = context.nextNumberId++
  context.lists.push({ id: numberId, ordered: list.tagName === 'OL' })
  return Array.from(list.children).filter(child => child.tagName === 'LI').map(item => {
    const inline = Array.from(item.childNodes).filter(child => !(child instanceof HTMLElement) || !['UL', 'OL', 'P', 'PRE', 'TABLE'].includes(child.tagName))
    const firstParagraph = item.querySelector(':scope > p')
    let content = paragraph(firstParagraph ? inlineWord(firstParagraph) : inline.map(node => inlineWord(node)).join(''), 'ListParagraph', `<w:numPr><w:ilvl w:val="${Math.min(depth, 8)}"/><w:numId w:val="${numberId}"/></w:numPr>`)
    for (const child of Array.from(item.children)) {
      if (child === firstParagraph || !['UL', 'OL', 'P', 'PRE', 'TABLE'].includes(child.tagName)) continue
      content += wordBlock(child, context, depth + (['UL', 'OL'].includes(child.tagName) ? 1 : 0))
    }
    return content
  }).join('')
}
function wordTable(table: HTMLElement): string {
  const rows = Array.from(table.querySelectorAll('tr')), columns = Math.max(1, ...rows.map(row => row.children.length)), cellWidth = Math.floor(9000 / columns)
  return `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="5000" w:type="pct"/><w:tblLook w:val="04A0"/></w:tblPr><w:tblGrid>${Array.from({ length: columns }, () => `<w:gridCol w:w="${cellWidth}"/>`).join('')}</w:tblGrid>${rows.map(row => `<w:tr>${Array.from(row.children).map(cell => `<w:tc><w:tcPr><w:tcW w:w="${cellWidth}" w:type="dxa"/>${cell.tagName === 'TH' ? '<w:shd w:fill="EDF3FE"/>' : ''}</w:tcPr>${paragraph(inlineWord(cell), cell.tagName === 'TH' ? 'TableHeader' : 'Normal')}</w:tc>`).join('')}</w:tr>`).join('')}</w:tbl>`
}
function wordBlock(node: Node, context: WordContext, depth = 0, overrideStyle?: string): string {
  if (node.nodeType === Node.TEXT_NODE) return node.textContent?.trim() ? paragraph(wordRun(node.textContent), overrideStyle) : ''
  if (!(node instanceof HTMLElement)) return Array.from(node.childNodes).map(child => wordBlock(child, context, depth, overrideStyle)).join('')
  if (/^H[1-6]$/.test(node.tagName)) return paragraph(inlineWord(node), `Heading${node.tagName.slice(1)}`)
  if (node.tagName === 'PRE') return (node.textContent || '').replace(/\n$/, '').split('\n').map(line => paragraph(wordRun(line, { code: true }), 'Code')).join('')
  if (node.tagName === 'UL' || node.tagName === 'OL') return wordList(node, depth, context)
  if (node.tagName === 'TABLE') return wordTable(node)
  if (node.tagName === 'BLOCKQUOTE') return Array.from(node.childNodes).map(child => wordBlock(child, context, depth, 'Quote')).join('')
  if (node.tagName === 'HR') return paragraph('', 'Normal', '<w:pBdr><w:bottom w:val="single" w:sz="4" w:space="8" w:color="D1D5DB"/></w:pBdr>')
  return paragraph(inlineWord(node), overrideStyle || 'Normal')
}

const xmlHeader = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
const wordNamespace = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main'
function wordStyles(): string {
  const headings = Array.from({ length: 6 }, (_, index) => `<w:style w:type="paragraph" w:styleId="Heading${index + 1}"><w:name w:val="heading ${index + 1}"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="120"/></w:pPr><w:rPr><w:b/><w:sz w:val="${[36, 30, 26, 24, 22, 22][index]}"/></w:rPr></w:style>`).join('')
  return `${xmlHeader}<w:styles xmlns:w="${wordNamespace}"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Microsoft YaHei"/><w:sz w:val="22"/><w:lang w:val="en-US" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="320" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="44"/></w:rPr></w:style>${headings}<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="0" w:after="0" w:line="260" w:lineRule="auto"/><w:shd w:fill="F5F6F8"/></w:pPr></w:style><w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="360"/><w:pBdr><w:left w:val="single" w:sz="12" w:space="8" w:color="D1D5DB"/></w:pBdr></w:pPr></w:style><w:style w:type="paragraph" w:styleId="TableHeader"><w:name w:val="Table Header"/><w:basedOn w:val="Normal"/><w:rPr><w:b/></w:rPr></w:style><w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblBorders>${['top', 'left', 'bottom', 'right', 'insideH', 'insideV'].map(edge => `<w:${edge} w:val="single" w:sz="4" w:color="D1D5DB"/>`).join('')}</w:tblBorders></w:tblPr></w:style></w:styles>`
}
function wordNumbering(context: WordContext): string {
  const definitions = [1, 2].map(id => `<w:abstractNum w:abstractNumId="${id}"><w:multiLevelType w:val="multilevel"/>${Array.from({ length: 9 }, (_, level) => `<w:lvl w:ilvl="${level}"><w:start w:val="1"/><w:numFmt w:val="${id === 1 ? 'bullet' : 'decimal'}"/><w:lvlText w:val="${id === 1 ? '•' : `%${level + 1}.`}"/><w:lvlJc w:val="left"/><w:pPr><w:tabs><w:tab w:val="num" w:pos="${720 * (level + 1)}"/></w:tabs><w:ind w:left="${720 * (level + 1)}" w:hanging="360"/></w:pPr></w:lvl>`).join('')}</w:abstractNum>`).join('')
  const lists = context.lists.map(list => `<w:num w:numId="${list.id}"><w:abstractNumId w:val="${list.ordered ? 2 : 1}"/></w:num>`).join('')
  return `${xmlHeader}<w:numbering xmlns:w="${wordNamespace}">${definitions}${lists}</w:numbering>`
}

/** A real OOXML package: no HTML masquerading as Word, macros or external relationships. */
export function buildChatDocx(input: ChatExportInput): Uint8Array {
  const messages = selected(input)
  const context: WordContext = { nextNumberId: 1, lists: [] }
  const body = paragraph(wordRun(exportTitle(input)), 'Title') + messages.map(message => paragraph(wordRun(roleName(message)), 'Heading2') + wordBlock(messageFragment(message), context)).join('')
  const documentXml = `${xmlHeader}<w:document xmlns:w="${wordNamespace}"><w:body>${body}<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="567" w:footer="567" w:gutter="0"/></w:sectPr></w:body></w:document>`
  const files: Record<string, Uint8Array> = {
    '[Content_Types].xml': strToU8(`${xmlHeader}<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/></Types>`),
    '_rels/.rels': strToU8(`${xmlHeader}<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`),
    'word/document.xml': strToU8(documentXml),
    'word/styles.xml': strToU8(wordStyles()),
    'word/numbering.xml': strToU8(wordNumbering(context)),
    'word/_rels/document.xml.rels': strToU8(`${xmlHeader}<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/></Relationships>`),
  }
  return zipSync(files, { level: 6 })
}

export function buildChatPrintHtml(input: ChatExportInput): string {
  const body = selected(input).map(message => `<section><h2>${roleName(message)}</h2>${(() => { const template = document.createElement('template'); template.content.append(messageFragment(message)); return template.innerHTML })()}</section>`).join('')
  return `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src 'none'; font-src 'none'; connect-src 'none'; base-uri 'none'; form-action 'none'"><title>${escapeHtml(exportTitle(input))}</title><style>@page{size:A4;margin:20mm}*{box-sizing:border-box}body{margin:0;color:#161616;font:11pt/1.65 system-ui,-apple-system,'Microsoft YaHei',sans-serif;overflow-wrap:anywhere}h1{font-size:22pt;line-height:1.3}h2{font-size:15pt;margin:24pt 0 10pt}h3,h4,h5,h6{break-after:avoid}section+section{border-top:1px solid #ddd;margin-top:20pt;padding-top:4pt}p{margin:0 0 10pt}pre{white-space:pre-wrap;background:#f5f6f8;padding:10pt;font-size:9pt}code{font-family:ui-monospace,monospace}table{width:100%;border-collapse:collapse;margin:12pt 0}th,td{border:1px solid #ccc;padding:6pt;text-align:left;vertical-align:top}th{background:#edf3fe}tr{break-inside:avoid}blockquote{border-left:3px solid #ccc;margin-left:0;padding-left:12pt;color:#555}a{color:inherit;text-decoration:underline}hr{border:0;border-top:1px solid #ddd}ul,ol{padding-left:22pt}</style></head><body><h1>${escapeHtml(exportTitle(input))}</h1>${body}</body></html>`
}

export function createChatExporter() {
  let current: { cancel: () => void } | undefined
  let disposed = false
  function dispose() { disposed = true; current?.cancel(); current = undefined }
  async function download(format: ChatExportFormat, input: ChatExportInput): Promise<void> {
    if (disposed) throw new DOMException('导出已取消', 'AbortError')
    if (current) throw new Error('请先完成当前打印')
    if (format === 'pdf') {
      const html = buildChatPrintHtml(input)
      return new Promise<void>((resolve, reject) => {
        const frame = document.createElement('iframe')
        frame.title = '对话打印预览'
        frame.setAttribute('aria-hidden', 'true')
        frame.setAttribute('sandbox', 'allow-same-origin allow-modals')
        Object.assign(frame.style, { position: 'fixed', width: '1px', height: '1px', right: '0', bottom: '0', border: '0', opacity: '0', pointerEvents: 'none' })
        let timeout: ReturnType<typeof setTimeout> | undefined, done = false
        const finish = (error?: Error) => { if (done) return; done = true; if (timeout) clearTimeout(timeout); frame.contentWindow?.removeEventListener('afterprint', afterPrint); frame.onload = null; frame.onerror = null; frame.remove(); current = undefined; if (error) reject(error); else resolve() }
        const afterPrint = () => finish()
        current = { cancel: () => finish(new DOMException('导出已取消', 'AbortError')) }
        frame.onload = () => {
          if (done || disposed) return
          if (timeout) clearTimeout(timeout)
          const target = frame.contentWindow
          if (!target) { finish(new Error('无法打开打印预览')); return }
          target.addEventListener('afterprint', afterPrint, { once: true })
          timeout = setTimeout(() => finish(), 5 * 60 * 1000)
          try { target.focus(); target.print() } catch { finish(new Error('无法打开打印窗口，请检查浏览器设置')) }
        }
        frame.onerror = () => finish(new Error('无法打开打印预览'))
        timeout = setTimeout(() => finish(new Error('打印预览加载超时，请重试')), 15000)
        frame.srcdoc = html
        document.body.append(frame)
      })
    }
    if (format === 'docx') {
      const bytes = buildChatDocx(input)
      saveAs(new Blob([Uint8Array.from(bytes).buffer], { type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' }), chatExportFilename(input.title, format))
    } else {
      const content = format === 'md' ? buildChatMarkdown(input) : buildChatText(input)
      saveAs(new Blob([content], { type: format === 'md' ? 'text/markdown;charset=utf-8' : 'text/plain;charset=utf-8' }), chatExportFilename(input.title, format))
    }
  }
  return { download, dispose }
}
