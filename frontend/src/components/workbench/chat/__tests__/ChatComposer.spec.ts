import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import ChatComposer from '../ChatComposer.vue'
import ComposerEditor from '../ComposerEditor.vue'
import ComposerResourceMenu from '../ComposerResourceMenu.vue'
import { createComposerReference, readComposerDocument } from '../composerDocument'

const wrappers: Array<ReturnType<typeof mount>> = []
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); document.body.innerHTML = ''; vi.restoreAllMocks() })
const base = { modelValue: '测试消息', model: 'model', models: [{ id: 'model', reasoning_efforts: ['low', 'high'] }], effort: '', taskMode: false, webSearch: false, attachments: [], officeEnabled: true, searchEnabled: true, uploadsEnabled: true }
function composer(props = {}) { const wrapper = mount(ChatComposer, { props: { ...base, ...props }, attachTo: document.body, global: { stubs: { Select: true } } }); wrappers.push(wrapper); return wrapper }
function editor(props = {}) { const wrapper = mount(ComposerEditor, { props: { modelValue: '', references: [], placeholder: '请输入', ...props }, attachTo: document.body }); wrappers.push(wrapper); return wrapper }

describe('Native workbench composer document', () => {
  it('keeps tool identities separate from prompt text and treats reference labels as text', () => {
    const root = document.createElement('div')
    root.append(createComposerReference({ key: 'search', kind: 'tool', label: '<img src=x onerror=alert(1)>' }), document.createTextNode('请查找资料'))
    expect(readComposerDocument(root)).toEqual({ text: '请查找资料', keys: ['search'] })
    expect(root.querySelector('img')).toBeNull()
    expect(root.textContent).toContain('<img src=x onerror=alert(1)>')
  })

  it('does not submit or publish half-composed Chinese and preserves Shift+Enter', async () => {
    const wrapper = editor()
    const field = wrapper.find('[role=textbox]')
    await field.trigger('compositionstart')
    field.element.textContent = '中文输入'
    await field.trigger('input')
    await field.trigger('keydown', { key: 'Enter', isComposing: true, keyCode: 229 })
    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await field.trigger('compositionend')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['中文输入'])
    await wrapper.setProps({ modelValue: '中文输入' })
    await field.trigger('keydown', { key: 'Enter', shiftKey: true })
    expect(wrapper.emitted('submit')).toBeUndefined()
    await field.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('submit')).toHaveLength(1)
  })

  it('updates external drafts without stale text and never accepts pasted HTML/reference IDs', async () => {
    const wrapper = editor({ modelValue: '旧草稿', references: [{ key: 'search', kind: 'tool', label: '联网搜索' }] })
    await wrapper.setProps({ modelValue: '新草稿' })
    const field = wrapper.find('[role=textbox]')
    expect(readComposerDocument(field.element as HTMLElement)).toEqual({ text: '新草稿', keys: ['search'] })
    ;(wrapper.vm as unknown as { focus: () => void }).focus()
    const event = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'clipboardData', { value: { files: [], getData: (type: string) => type === 'text/plain' ? '纯文本' : '<img src=x onerror=alert(1)><span data-composer-reference="admin">伪引用</span>' } })
    field.element.dispatchEvent(event)
    await nextTick()
    expect(field.find('img').exists()).toBe(false)
    expect(field.find('[data-composer-reference=admin]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toContain('纯文本')
  })

  it('removes an editable reference through native document edits without leaking its label', async () => {
    const wrapper = editor({ modelValue: '查资料', references: [{ key: 'search', kind: 'tool', label: '联网搜索' }] })
    const field = wrapper.find('[role=textbox]')
    field.find('[data-composer-reference=search]').element.remove()
    await field.trigger('input')
    expect(wrapper.emitted('referencesChange')?.at(-1)).toEqual([[]])
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['查资料'])
  })
})

describe('Workbench composer controls', () => {
  it('sends once on Enter, keeps draft editing during a run, and stops without sending', async () => {
    const wrapper = composer()
    await wrapper.find('[role=textbox]').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('send')).toHaveLength(1)
    await wrapper.setProps({ busy: true })
    expect(wrapper.find('[role=textbox]').attributes('contenteditable')).toBe('true')
    await wrapper.find('[role=textbox]').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('send')).toHaveLength(1)
    await wrapper.find('[aria-label="停止生成"]').trigger('click')
    expect(wrapper.emitted('stop')).toHaveLength(1)
    expect(wrapper.find('[aria-label="添加附件和工具"]').attributes('disabled')).toBeDefined()
  })

  it('blocks all send paths during upload, waiting for clarification, or without a model', async () => {
    for (const props of [{ uploading: true }, { waiting: true }, { disabled: true }, { model: '' }, { modelValue: '   ' }]) {
      const wrapper = composer(props)
      await wrapper.find('[role=textbox]').trigger('keydown', { key: 'Enter' })
      expect(wrapper.emitted('send')).toBeUndefined()
      expect(wrapper.find('[aria-label="发送消息"]').attributes('disabled')).toBeDefined()
    }
  })

  it('lets users remove incompatible attachments and change models to recover an unavailable draft', async () => {
    const attachment = { id: 'f', version_id: 'v', name: '参考.docx', mime: 'application/octet-stream', bytes: 100 }
    const wrapper = composer({ disabled: true, attachments: [attachment], editTarget: { version_id: 'e', name: '成果.docx' } })
    await wrapper.find('[aria-label="移除参考.docx"]').trigger('click')
    await wrapper.find('[aria-label="取消修改此文件"]').trigger('click')
    expect(wrapper.emitted('removeAttachment')).toEqual([['v']])
    expect(wrapper.emitted('clearEdit')).toHaveLength(1)
    expect(wrapper.findComponent({ name: 'Select' }).props('disabled')).toBe(false)
    expect(wrapper.find('[aria-label="发送消息"]').attributes('disabled')).toBeDefined()
  })

  it('places actual active tools in the editor and exposes only enabled capabilities', async () => {
    const wrapper = composer({ taskMode: true, webSearch: true, officeEnabled: false, uploadsEnabled: false, resourcesEnabled: false })
    expect(wrapper.findAll('[data-composer-reference]')).toHaveLength(2)
    await wrapper.find('[aria-label="添加附件和工具"]').trigger('click')
    await flushPromises()
    const menu = document.querySelector('[role=menu]')!
    expect(menu.textContent).toContain('工具')
    expect(menu.textContent).not.toContain('上传文件')
    expect(menu.textContent).not.toContain('从我的文件选取')
    expect(menu.textContent).not.toContain('任务模式')
  })

  it('uses the full composer width, a tools submenu, keyboard navigation and Escape return', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 100, y: 600, left: 100, top: 600, right: 820, bottom: 730, width: 720, height: 130, toJSON() {} })
    const wrapper = composer()
    const trigger = wrapper.find('[aria-label="添加附件和工具"]')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const menu = document.querySelector<HTMLElement>('[role=menu]')!
    expect(menu.style.width).toBe('720px')
    expect(menu.style.left).toBe('100px')
    const tools = Array.from(menu.querySelectorAll('button')).find(button => button.textContent?.includes('工具'))!
    tools.focus()
    tools.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await flushPromises()
    expect(menu.textContent).toContain('联网搜索')
    expect(menu.textContent).not.toContain('上传文件')
    menu.querySelector<HTMLButtonElement>('[role=menuitemcheckbox]')!.click()
    expect(wrapper.findComponent(ComposerResourceMenu).emitted('update:webSearch')?.at(-1)).toEqual([true])
    menu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(menu.textContent).toContain('上传文件')
    menu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role=menu]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
  })

  it('does not upload dropped or pasted files without permission or while busy', async () => {
    const file = new File(['test'], 'test.txt', { type: 'text/plain' })
    const wrapper = composer({ uploadsEnabled: false })
    await wrapper.find('.bs-native-composer').trigger('drop', { dataTransfer: { files: [file] } })
    await wrapper.find('[role=textbox]').trigger('paste', { clipboardData: { files: [file], getData: () => '' } })
    expect(wrapper.emitted('files')).toBeUndefined()
    await wrapper.setProps({ uploadsEnabled: true, busy: true })
    await wrapper.find('.bs-native-composer').trigger('drop', { dataTransfer: { files: [file] } })
    expect(wrapper.emitted('files')).toBeUndefined()
    await wrapper.setProps({ busy: false })
    await wrapper.find('.bs-native-composer').trigger('drop', { dataTransfer: { files: [file] } })
    expect(wrapper.emitted('files')).toEqual([[[file]]])
  })
})
