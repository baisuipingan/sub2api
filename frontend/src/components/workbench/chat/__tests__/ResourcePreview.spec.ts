import { createPinia, defineStore, setActivePinia } from 'pinia'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ResourcePreview from '../ResourcePreview.vue'
import { buildChatDocx } from '../chatExport'
import { zipSync } from 'fflate'
import { useAuthStore } from '@/stores/auth'
import type { ChatArtifact } from '@/types/workbenchChat'

const mocks = vi.hoisted(() => ({ access: vi.fn(), printPreview: vi.fn(), fetch: vi.fn(), create: vi.fn(), revoke: vi.fn(), save: vi.fn() }))
vi.mock('file-saver', () => ({ saveAs: mocks.save }))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: { access: mocks.access, printPreview: mocks.printPreview } }))
vi.mock('@/stores/workbenchChat', () => ({ chatError: (error: Error) => error.message }))
vi.mock('@/stores/auth', () => ({ useAuthStore: defineStore('preview-test-auth', () => ({ user: ref<{ id: number } | null>({ id: 1 }) })) }))
const file = (id = 'pdf'): ChatArtifact => ({ id, version_id: id, name: `${id}.pdf`, mime: 'application/pdf', bytes: 100 })
const wrappers: ReturnType<typeof mount>[] = []
function render(value: ChatArtifact | null = file()) { const wrapper = mount(ResourcePreview, { props: { files: value ? [value] : [], file: value }, global: { stubs: { Icon: true, ChatMarkdown: true } } }); wrappers.push(wrapper); return wrapper }
function pdfResponse(value = '%PDF-1.4\npreview', type = 'application/pdf') { return new Response(value, { headers: { 'Content-Type': type } }) }
const wordFile = (): ChatArtifact => ({
  ...file('word'),
  name: '天气报告.docx',
  mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  preview_kind: 'unsupported',
})
function wordResponse(bytes = buildChatDocx({ messages: [{ id: 'msg', role: 'assistant', content: '上海天气：晴转多云', sequence: 1, conversation_id: 'chat', run_id: 'run', created_at: '' }] })) {
  return new Response(bytes)
}
function wordAccess() {
  return { url: 'https://private-storage.test/word', preview_kind: 'unsupported', expires_at: new Date(Date.now() + 300000).toISOString() }
}

describe('private resource previews', () => {
  beforeEach(() => {
    setActivePinia(createPinia()); vi.clearAllMocks()
    vi.stubGlobal('fetch', mocks.fetch)
    mocks.create.mockReturnValue('blob:https://site.test/local-preview')
    vi.stubGlobal('URL', class extends URL { static createObjectURL = mocks.create; static revokeObjectURL = mocks.revoke })
    mocks.access.mockResolvedValue({ url: 'https://private-storage.test/original', preview_url: 'https://private-storage.test/preview', preview_kind: 'pdf', expires_at: new Date(Date.now() + 300000).toISOString() })
    mocks.fetch.mockImplementation(async () => pdfResponse())
  })
  afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks(); vi.unstubAllGlobals() })
  it('fetches an authorized PDF into a typed local blob and reuses it when reopened', async () => {
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toBe('blob:https://site.test/local-preview')
    expect(wrapper.get('iframe').attributes('sandbox')).toBeUndefined()
    expect(wrapper.get('iframe').attributes('referrerpolicy')).toBe('no-referrer')
    expect(mocks.create.mock.calls[0]?.[0].type).toBe('application/pdf')
    await wrapper.setProps({ file: null }); await wrapper.setProps({ file: file() }); await flushPromises()
    expect(mocks.access).toHaveBeenCalledTimes(1); expect(mocks.fetch).toHaveBeenCalledTimes(1)
  })
  it.each([['<html>not a PDF</html>', 'application/pdf'], ['%PDF-1.4\npreview', 'text/html']])('never embeds a non-PDF response (%s / %s)', async (body, type) => {
    mocks.fetch.mockResolvedValueOnce(pdfResponse(body, type))
    const wrapper = render(); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('不是有效的 PDF')
    expect(mocks.create).not.toHaveBeenCalled()
  })
  it('rejects oversized previews before allocating a blob', async () => {
    mocks.fetch.mockResolvedValueOnce(new Response('%PDF-', { headers: { 'Content-Type': 'application/pdf', 'Content-Length': String(25 * 1024 * 1024) } }))
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('超过 24 MB')
    expect(mocks.create).not.toHaveBeenCalled()
  })
  it('evicts bounded previews and revokes private blob URLs on logout and unmount', async () => {
    mocks.create.mockImplementation(() => `blob:https://site.test/${mocks.create.mock.calls.length}`)
    const wrapper = render(); await flushPromises()
    for (let index = 1; index < 9; index++) { await wrapper.setProps({ file: file(`pdf-${index}`) }); await flushPromises() }
    expect(mocks.revoke).toHaveBeenCalledWith('blob:https://site.test/1')
    useAuthStore().user = null; await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(mocks.revoke).toHaveBeenCalledTimes(9)
    wrapper.unmount(); expect(mocks.revoke).toHaveBeenCalledTimes(9)
  })
  it('does not restore private bytes after logout while the response is in flight', async () => {
    let resolve!: (response: Response) => void
    mocks.fetch.mockImplementationOnce(() => new Promise<Response>(r => { resolve = r }))
    const wrapper = render(); await flushPromises()
    useAuthStore().user = null; resolve(pdfResponse()); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(mocks.create).not.toHaveBeenCalled()
  })
  it('switches preview/source and back to the file list without reading the object again', async () => {
    const value = file('notes'); value.name = 'notes.md'; value.mime = 'text/markdown'
    mocks.access.mockResolvedValue({ url: 'https://private-storage.test/notes', preview_kind: 'markdown', expires_at: new Date(Date.now() + 300000).toISOString() })
    mocks.fetch.mockResolvedValue(new Response('# 计划\n\n文档内容', { headers: { 'Content-Type': 'text/markdown' } }))
    const wrapper = render(value); await flushPromises()
    expect(wrapper.getComponent({ name: 'ChatMarkdown' }).exists()).toBe(true)
    await wrapper.findAll('[role="tab"]')[1]!.trigger('click')
    expect(wrapper.get('pre').text()).toContain('# 计划')
    await wrapper.findAll('[role="tab"]')[0]!.trigger('click')
    expect(mocks.fetch).toHaveBeenCalledTimes(1)
    await wrapper.get('button[aria-label="返回文件列表"]').trigger('click')
    expect(wrapper.emitted('preview')?.[0]).toEqual([null])
    await wrapper.setProps({ file: null }); await wrapper.setProps({ file: value }); await flushPromises()
    expect(wrapper.find('pre').exists()).toBe(false)
    expect(mocks.access).toHaveBeenCalledTimes(1)
    expect(mocks.fetch).toHaveBeenCalledTimes(1)
  })
  it('offers continue-edit only for editable office files outside read-only mode', async () => {
    const value = { ...file('office'), name: 'report.docx' }
    const wrapper = render(value); await flushPromises()
    await wrapper.get('.bso-continue-edit').trigger('click')
    expect(wrapper.emitted('edit')?.[0]).toEqual([value])
    await wrapper.setProps({ readOnly: true })
    expect(wrapper.find('.bso-continue-edit').exists()).toBe(false)
  })
  it('renders a DOCX from its signed original in a scriptless iframe and reuses it for download', async () => {
    mocks.access.mockResolvedValue(wordAccess())
    mocks.fetch.mockImplementation(async () => wordResponse())
    const value = wordFile()
    const wrapper = render(value)
    await vi.waitFor(() => expect(wrapper.find('iframe.bs-chat-docx-preview').exists()).toBe(true))
    const preview = wrapper.get('iframe')
    expect(preview.attributes('sandbox')).toBe('')
    expect(preview.attributes('srcdoc')).toContain('上海天气：晴转多云')
    expect(preview.attributes('srcdoc')).toContain("default-src 'none'")
    expect(mocks.fetch.mock.calls[0]?.[0]).toBe('https://private-storage.test/word')
    await wrapper.setProps({ fullscreen: true })
    await wrapper.setProps({ file: null })
    await wrapper.setProps({ file: value })
    await wrapper.get('[aria-label="下载文件"]').trigger('click')
    await flushPromises()
    expect(mocks.access).toHaveBeenCalledTimes(1)
    expect(mocks.fetch).toHaveBeenCalledTimes(1)
    expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ type: value.mime }), value.name)
  })
  it('prefers an existing PDF preview for a Word file', async () => {
    const wrapper = render(wordFile())
    await flushPromises()
    expect(wrapper.find('.bs-chat-pdf-preview').exists()).toBe(true)
    expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(false)
    expect(mocks.fetch.mock.calls[0]?.[0]).toBe('https://private-storage.test/preview')
  })
  it('loads original Draw.io XML only when its file opens and reuses it for the same version', async () => {
    const value: ChatArtifact = { ...file('diagram'), name: '流程.drawio', mime: 'application/xml', preview_kind: 'text' }
    const wrapper = render(null)
    mocks.access.mockResolvedValue({ ...wordAccess(), url: 'https://private-storage.test/diagram' })
    mocks.fetch.mockImplementation(async () => new Response('<mxfile><diagram><mxGraphModel><root><mxCell id="0"/><mxCell id="1" parent="0"/><mxCell id="2" value="开始" vertex="1" parent="1"><mxGeometry x="0" y="0" width="100" height="60" as="geometry"/></mxCell></root></mxGraphModel></diagram></mxfile>'))
    expect(mocks.fetch).not.toHaveBeenCalled()
    await wrapper.setProps({ file: value })
    await vi.waitFor(() => expect(wrapper.getComponent({ name: 'DrawioPreview' }).props('xml')).toContain('mxGraphModel'))
    await wrapper.setProps({ file: null })
    await wrapper.setProps({ file: value })
    await flushPromises()
    expect(mocks.fetch).toHaveBeenCalledTimes(1)
  })
  it('stops after the expanded XML exceeds the browser budget and retains original download', async () => {
    const bytes = zipSync({ 'word/document.xml': new Uint8Array(9 * 1024 * 1024) })
    mocks.access.mockResolvedValue(wordAccess())
    mocks.fetch.mockImplementation(async () => wordResponse(bytes))
    const wrapper = render(wordFile())
    await vi.waitFor(() => expect(wrapper.get('[role="alert"]').text()).toContain('预览内容过大'))
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.get('[aria-label="下载文件"]').exists()).toBe(true)
  })
  it('refreshes a rejected Word link once and removes the preview on account change', async () => {
    mocks.access.mockResolvedValue(wordAccess())
    mocks.fetch.mockResolvedValueOnce(new Response(null, { status: 403 }))
    mocks.fetch.mockImplementation(async () => wordResponse())
    const wrapper = render(wordFile())
    await vi.waitFor(() => expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(true))
    expect(mocks.access).toHaveBeenCalledTimes(2)
    expect(mocks.fetch).toHaveBeenCalledTimes(2)
    useAuthStore().user = null
    await flushPromises()
    expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(false)
  })
  it('generates print layout only on request and retains the quick preview and original download', async () => {
    const value = wordFile()
    mocks.access.mockResolvedValue({ ...wordAccess(), can_render_print_preview: true })
    mocks.fetch.mockImplementation(async (url: string) => url.endsWith('/print') ? pdfResponse() : wordResponse())
    mocks.printPreview.mockResolvedValueOnce({ status: 'pending', retry_after_ms: 1 })
    mocks.printPreview.mockResolvedValue({ status: 'ready', preview_url: 'https://private-storage.test/print' })
    const wrapper = render(value)
    await vi.waitFor(() => expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(true))
    expect(mocks.printPreview).not.toHaveBeenCalled()
    await wrapper.get('[aria-label="打印版预览"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(true)
    expect(wrapper.get('[aria-label="正在准备打印版预览"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[aria-label="下载文件"]').trigger('click')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ type: value.mime }), value.name)
    await vi.waitFor(() => expect(wrapper.find('.bs-chat-pdf-preview').exists()).toBe(true), 2000)
    expect(mocks.printPreview).toHaveBeenCalledTimes(2)
    expect(mocks.printPreview).toHaveBeenNthCalledWith(1, value.version_id, expect.any(AbortSignal), true)
    expect(mocks.printPreview).toHaveBeenNthCalledWith(2, value.version_id, expect.any(AbortSignal), false)
    await wrapper.get('[aria-label="返回快速预览"]').trigger('click')
    expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(true)
    await wrapper.get('[aria-label="打印版预览"]').trigger('click')
    expect(wrapper.find('.bs-chat-pdf-preview').exists()).toBe(true)
    expect(mocks.printPreview).toHaveBeenCalledTimes(2)
    expect(mocks.fetch).toHaveBeenCalledTimes(2)
  })
  it('shows a print error without hiding the quick preview', async () => {
    mocks.access.mockResolvedValue({ ...wordAccess(), can_render_print_preview: true })
    mocks.fetch.mockImplementation(async () => wordResponse())
    mocks.printPreview.mockRejectedValueOnce(new Error('打印版转换暂不可用'))
    const wrapper = render(wordFile())
    await vi.waitFor(() => expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(true))
    await wrapper.get('[aria-label="打印版预览"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('打印版转换暂不可用')
    expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(true)
    expect(wrapper.get('[aria-label="下载文件"]').exists()).toBe(true)
  })
  it('cancels a print request when changing files and ignores the stale response', async () => {
    const value = wordFile()
    mocks.access.mockResolvedValue({ ...wordAccess(), can_render_print_preview: true })
    mocks.fetch.mockImplementation(async () => wordResponse())
    let respond!: (value: { status: string; preview_url: string }) => void
    mocks.printPreview.mockImplementationOnce(() => new Promise(resolve => { respond = resolve }))
    const wrapper = render(value)
    await vi.waitFor(() => expect(wrapper.find('.bs-chat-docx-preview').exists()).toBe(true))
    await wrapper.get('[aria-label="打印版预览"]').trigger('click')
    const signal = mocks.printPreview.mock.calls[0][1] as AbortSignal
    await wrapper.setProps({ file: null })
    expect(signal.aborted).toBe(true)
    respond({ status: 'ready', preview_url: 'https://private-storage.test/late' })
    await flushPromises()
    expect(mocks.fetch).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.bs-chat-pdf-preview').exists()).toBe(false)
  })
})
