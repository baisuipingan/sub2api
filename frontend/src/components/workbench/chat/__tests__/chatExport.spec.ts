import { afterEach, describe, expect, it, vi } from 'vitest'
import { strFromU8, unzipSync } from 'fflate'
import { saveAs } from 'file-saver'
import { buildChatDocx, buildChatMarkdown, buildChatPrintHtml, buildChatText, chatExportFilename, createChatExporter } from '../chatExport'
import type { ChatMessage } from '@/types/workbenchChat'

vi.mock('file-saver', () => ({ saveAs: vi.fn() }))
const message = (id: string, role: ChatMessage['role'], content: ChatMessage['content']): ChatMessage => ({ id, role, content, conversation_id: 'conversation', run_id: 'run', sequence: 1, created_at: '2026-11-19T00:00:00Z' })
afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks(); vi.clearAllMocks(); vi.useRealTimers() })

describe('Local chat document exports', () => {
  it('creates a genuine, well formed OOXML DOCX with Chinese, paragraphs, lists, code and tables', () => {
    const content = '# 一级标题\n\n中文 **粗体** 与 *斜体*。\n\n- 第一点\n- 第二点\n\n1. 第一步\n2. 第二步\n\n```python\nprint("中文 <测试>")\n  保留缩进\n```\n\n| 名称 | 数量 |\n| --- | --- |\n| 苹果 | 3 |\n\n> 引用内容'
    const bytes = buildChatDocx({ title: '办公对话', messages: [message('u', 'user', '生成文档'), message('a', 'assistant', content)] })
    expect(Array.from(bytes.slice(0, 2))).toEqual([80, 75])
    const entries = unzipSync(bytes)
    expect(Object.keys(entries).sort()).toEqual(['[Content_Types].xml', '_rels/.rels', 'word/_rels/document.xml.rels', 'word/document.xml', 'word/numbering.xml', 'word/styles.xml'].sort())
    for (const [path, data] of Object.entries(entries)) {
      const source = strFromU8(data)
      expect(new DOMParser().parseFromString(source, 'application/xml').querySelector('parsererror'), path).toBeNull()
      expect(source, path).not.toContain('TargetMode="External"')
    }
    const source = strFromU8(entries['word/document.xml']!)
    const xml = new DOMParser().parseFromString(source, 'application/xml')
    expect(xml.documentElement.namespaceURI).toBe('http://schemas.openxmlformats.org/wordprocessingml/2006/main')
    expect(xml.documentElement.textContent).toContain('办公对话')
    expect(xml.documentElement.textContent).toContain('中文 <测试>')
    expect(source).toContain('w:pStyle w:val="Heading1"')
    expect(source).toContain('<w:b/>')
    expect(source).toContain('<w:i/>')
    expect(source).toContain('<w:numPr>')
    expect(source).toContain('w:pStyle w:val="Code"')
    expect(source).toContain('xml:space="preserve">  保留缩进')
    expect(source).toContain('<w:tbl>')
    expect(source).toContain('w:pStyle w:val="Quote"')
    expect(strFromU8(entries['word/styles.xml']!)).toContain('w:eastAsia="Microsoft YaHei"')
  })

  it('omits system messages, deduplicates IDs, escapes XML and never embeds remote images or relationships', () => {
    const attack = '<script>secret()</script><img src="https://evil.example/pixel" onerror="alert(1)"><iframe src="https://evil.example/frame"></iframe>\n\n![示意图](https://evil.example/image.png)\n\n[点击](javascript:alert(1))\n\n正文 & <测试> \u0001\ud800'
    const input = { title: '<标题 & 引号">', messages: [message('s', 'system', '系统秘密'), message('a', 'assistant', attack), message('a', 'assistant', '重复内容')] }
    const entries = unzipSync(buildChatDocx(input))
    const source = Object.values(entries).map(bytes => strFromU8(bytes)).join('\n')
    expect(source).not.toContain('系统秘密')
    expect(source).not.toContain('重复内容')
    expect(source).not.toContain('evil.example')
    expect(source).not.toContain('javascript:')
    expect(source).not.toContain('<script')
    expect(source).not.toContain('onerror')
    expect(source).not.toContain('TargetMode="External"')
    expect(source).toContain('[图片：示意图]')
    const xml = new DOMParser().parseFromString(strFromU8(entries['word/document.xml']!), 'application/xml')
    expect(xml.querySelector('parsererror')).toBeNull()
    expect(xml.documentElement.textContent).toContain('<标题 & 引号">')
  })

  it('restarts independent ordered lists and defines each numbering relationship', () => {
    const entries = unzipSync(buildChatDocx({ messages: [message('a', 'assistant', '1. 第一列表\n\n结束。\n\n1. 第二列表')] }))
    const document = strFromU8(entries['word/document.xml']!), numbering = strFromU8(entries['word/numbering.xml']!)
    expect(document).toContain('<w:numId w:val="1"/>')
    expect(document).toContain('<w:numId w:val="2"/>')
    expect(numbering).toContain('<w:num w:numId="1">')
    expect(numbering).toContain('<w:num w:numId="2">')
  })

  it('exports only the supplied user/assistant content and file names in Markdown and plain text', () => {
    const input = { title: '选中的对话', messages: [message('s', 'system', '不可导出'), message('a', 'assistant', { text: '**你好**\n\n| A | B |\n|---|---|\n|1|2|', artifacts: [{ id: 'r', version_id: 'v', name: '成果.docx', mime: 'application/octet-stream', bytes: 1 }] })] }
    expect(buildChatMarkdown(input)).toContain('**你好**')
    expect(buildChatMarkdown(input)).toContain('文件：成果.docx')
    expect(buildChatText(input)).toContain('你好')
    expect(buildChatText(input)).toContain('A\tB\n1\t2')
    expect(buildChatText(input)).not.toContain('**')
    expect(buildChatText(input)).not.toContain('不可导出')
    expect(() => buildChatDocx({ messages: [message('s', 'system', '仅系统')] })).toThrow('请先选择')
    expect(chatExportFilename('CON', 'docx')).toBe('对话记录_CON.docx')
    expect(chatExportFilename('../坏:名字/', 'txt')).toBe('.._坏_名字_.txt')
  })

  it('downloads local Word/Markdown/text files without fetching or signing any objects', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch')
    const exporter = createChatExporter(), input = { messages: [message('a', 'assistant', '内容')] }
    await exporter.download('docx', input)
    await exporter.download('md', input)
    await exporter.download('txt', input)
    expect(saveAs).toHaveBeenCalledTimes(3)
    expect(vi.mocked(saveAs).mock.calls.map(call => call[1])).toEqual(['对话记录.docx', '对话记录.md', '对话记录.txt'])
    expect((vi.mocked(saveAs).mock.calls[0]![0] as Blob).type).toBe('application/vnd.openxmlformats-officedocument.wordprocessingml.document')
    expect(fetch).not.toHaveBeenCalled()
    exporter.dispose()
  })
})

describe('Offline PDF printing lifecycle', () => {
  it('builds a restrictive offline print document with no active or remote resources', () => {
    const html = buildChatPrintHtml({ title: '<恶意标题>', messages: [message('s', 'system', '系统秘密'), message('a', 'assistant', '![远程图片](https://evil.example/x)\n\n<a href="https://evil.example" onclick="alert(1)">来源</a><video src="https://evil.example/v"></video><style>body{background:url(https://evil.example/style)}</style><script>alert(1)</script>')] })
    const parsed = new DOMParser().parseFromString(html, 'text/html')
    expect(parsed.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute('content')).toContain("default-src 'none'")
    expect(parsed.querySelector('script,img,video,iframe,object,embed,[src],[href],[onclick]')).toBeNull()
    expect(html).not.toContain('evil.example')
    expect(parsed.body.textContent).not.toContain('系统秘密')
    expect(parsed.title).toBe('<恶意标题>')
  })

  it('prints an offline sandboxed iframe and releases it after the system print dialog closes', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch'), exporter = createChatExporter()
    const pending = exporter.download('pdf', { messages: [message('a', 'assistant', '打印内容')] })
    const frame = document.querySelector('iframe')!
    expect(frame.getAttribute('src')).toBeNull()
    expect(frame.getAttribute('sandbox')).toBe('allow-same-origin allow-modals')
    expect(frame.srcdoc).toContain('打印内容')
    const print = vi.fn(), focus = vi.fn()
    Object.defineProperty(frame.contentWindow, 'print', { value: print, configurable: true })
    Object.defineProperty(frame.contentWindow, 'focus', { value: focus, configurable: true })
    frame.dispatchEvent(new Event('load'))
    expect(print).toHaveBeenCalledOnce()
    expect(fetch).not.toHaveBeenCalled()
    frame.contentWindow!.dispatchEvent(new Event('afterprint'))
    await pending
    expect(document.querySelector('iframe')).toBeNull()
    exporter.dispose()
  })

  it('cleans pending print documents on disposal and cannot export after account teardown', async () => {
    const exporter = createChatExporter()
    const pending = exporter.download('pdf', { messages: [message('a', 'assistant', '私有内容')] })
    expect(document.querySelector('iframe')).not.toBeNull()
    const result = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    exporter.dispose()
    await result
    expect(document.querySelector('iframe')).toBeNull()
    await expect(exporter.download('md', { messages: [message('a', 'assistant', '内容')] })).rejects.toMatchObject({ name: 'AbortError' })
  })

  it('bounds a failed iframe load and releases resources', async () => {
    vi.useFakeTimers()
    const exporter = createChatExporter()
    const pending = exporter.download('pdf', { messages: [message('a', 'assistant', '内容')] })
    document.querySelector('iframe')!.onload = null
    const result = expect(pending).rejects.toThrow('加载超时')
    await vi.advanceTimersByTimeAsync(15000)
    await result
    expect(document.querySelector('iframe')).toBeNull()
    exporter.dispose()
  })
})
