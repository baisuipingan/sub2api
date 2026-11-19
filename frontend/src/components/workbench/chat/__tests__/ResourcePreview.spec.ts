import { createPinia, defineStore, setActivePinia } from 'pinia'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ResourcePreview from '../ResourcePreview.vue'
import { useAuthStore } from '@/stores/auth'
import type { ChatArtifact } from '@/types/workbenchChat'

const mocks = vi.hoisted(() => ({ access: vi.fn(), fetch: vi.fn(), create: vi.fn(), revoke: vi.fn() }))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: { access: mocks.access } }))
vi.mock('@/stores/workbenchChat', () => ({ chatError: (error: Error) => error.message }))
vi.mock('@/stores/auth', () => ({ useAuthStore: defineStore('preview-test-auth', () => ({ user: ref<{ id: number } | null>({ id: 1 }) })) }))
const file = (id = 'pdf'): ChatArtifact => ({ id, version_id: id, name: `${id}.pdf`, mime: 'application/pdf', bytes: 100 })
const wrappers: ReturnType<typeof mount>[] = []
function render(value = file()) { const wrapper = mount(ResourcePreview, { props: { files: [value], file: value }, global: { stubs: { Icon: true, ChatMarkdown: true } } }); wrappers.push(wrapper); return wrapper }
function pdfResponse(value = '%PDF-1.4\npreview', type = 'application/pdf') { return new Response(value, { headers: { 'Content-Type': type } }) }

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
})
