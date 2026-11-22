import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import ChatExportToolbar from '../ChatExportToolbar.vue'
import ChatExportDialog from '../ChatExportDialog.vue'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import { saveAs } from 'file-saver'
import type { ChatMessage } from '@/types/workbenchChat'

vi.mock('file-saver', () => ({ saveAs: vi.fn() }))
const messages: ChatMessage[] = [{ id: 'a', role: 'assistant', content: '选中的内容', conversation_id: 'c', run_id: 'r', sequence: 1, created_at: '2026-11-19T00:00:00Z' }]
const wrappers: Array<ReturnType<typeof mount>> = []
beforeEach(() => { setActivePinia(createPinia()) })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); document.body.innerHTML = ''; vi.clearAllMocks(); vi.restoreAllMocks() })

describe('Native message export controls', () => {
  it('offers the four real formats, toggles select-all and exports only the supplied selection', async () => {
    const wrapper = mount(ChatExportToolbar, { props: { messages, title: '已选对话', allSelected: false }, attachTo: document.body })
    wrappers.push(wrapper)
    expect(wrapper.findAll('.bs-chat-export-desktop-formats button').map(button => button.attributes('aria-label'))).toEqual(['导出为 Word', '导出为 PDF', '导出为 Markdown', '导出为 TXT'])
    expect(wrapper.find('[aria-label="导出为 PDF"]').attributes('title')).toContain('另存为 PDF')
    await wrapper.find('input[type=checkbox]').setValue(true)
    expect(wrapper.emitted('toggleAll')).toHaveLength(1)
    await wrapper.find('[aria-label="导出为 Markdown"]').trigger('click')
    await flushPromises()
    expect(saveAs).toHaveBeenCalledOnce()
    expect(vi.mocked(saveAs).mock.calls[0]?.[1]).toBe('已选对话.md')
    expect(wrapper.emitted('exported')).toHaveLength(1)
  })

  it('opens a format-only sheet from the mobile entry without recreating message selection', async () => {
    const wrapper = mount(ChatExportToolbar, { props: { messages }, attachTo: document.body })
    wrappers.push(wrapper)
    await wrapper.find('[aria-label="导出到本地"]').trigger('click')
    await nextTick()
    expect(document.querySelector('[role=dialog]')?.textContent).toContain('选择导出格式')
    expect(document.querySelector('[role=dialog] input[type=checkbox]')).toBeNull()
    expect(document.querySelectorAll('.bs-chat-export-sheet>button')).toHaveLength(5)
    expect(document.querySelector('[role=dialog]')?.textContent).not.toContain('选中的内容')
  })

  it('cleans an active print iframe and closes selection when the account changes', async () => {
    const auth = useAuthStore()
    auth.$patch({ user: { id: 1 } as User })
    const wrapper = mount(ChatExportToolbar, { props: { messages }, attachTo: document.body })
    wrappers.push(wrapper)
    ;(wrapper.find('[aria-label="导出为 PDF"]').element as HTMLButtonElement).click()
    const frame = document.querySelector('iframe')!
    Object.defineProperty(frame.contentWindow, 'focus', { value: vi.fn(), configurable: true })
    Object.defineProperty(frame.contentWindow, 'print', { value: vi.fn(), configurable: true })
    auth.$patch({ user: { id: 2 } as User })
    await flushPromises()
    expect(document.querySelector('iframe')).toBeNull()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('exported')).toBeUndefined()
  })

  it('cancels and releases pending printing when its toolbar unmounts', async () => {
    const wrapper = mount(ChatExportToolbar, { props: { messages }, attachTo: document.body })
    ;(wrapper.find('[aria-label="导出为 PDF"]').element as HTMLButtonElement).click()
    const frame = document.querySelector('iframe')!
    Object.defineProperty(frame.contentWindow, 'focus', { value: vi.fn(), configurable: true })
    Object.defineProperty(frame.contentWindow, 'print', { value: vi.fn(), configurable: true })
    wrapper.unmount()
    await flushPromises()
    expect(document.querySelector('iframe')).toBeNull()
  })

  it('rejects empty/system-only selections and exports a Word file from the sheet', async () => {
    const wrapper = mount(ChatExportDialog, { props: { show: true, messages: [{ ...messages[0]!, role: 'system' }] }, attachTo: document.body })
    wrappers.push(wrapper)
    expect(document.querySelector<HTMLButtonElement>('.bs-chat-export-sheet>button')!.disabled).toBe(true)
    await wrapper.setProps({ messages })
    document.querySelector<HTMLButtonElement>('.bs-chat-export-sheet>button')!.click()
    await flushPromises()
    expect(saveAs).toHaveBeenCalledOnce()
    expect(vi.mocked(saveAs).mock.calls[0]?.[1]).toBe('对话记录.docx')
    expect(wrapper.emitted('exported')).toHaveLength(1)
  })
})
