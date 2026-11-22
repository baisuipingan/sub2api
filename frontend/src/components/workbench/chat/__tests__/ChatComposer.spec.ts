import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import ChatComposer from '../ChatComposer.vue'
import ComposerEditor from '../ComposerEditor.vue'
import { readComposerDocument } from '../composerDocument'
import { setCurrentBillingCurrencySettings } from '@/utils/billingCurrency'

const wrappers: Array<ReturnType<typeof mount>> = []
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); document.body.innerHTML = ''; vi.restoreAllMocks(); setCurrentBillingCurrencySettings() })
const base = { modelValue: '测试消息', model: 'model', models: [{ id: 'model', reasoning_efforts: ['low', 'high'] }], effort: '', webSearch: false, attachments: [], searchEnabled: true, uploadsEnabled: true }
function composer(props = {}) { const wrapper = mount(ChatComposer, { props: { ...base, ...props }, attachTo: document.body, global: { stubs: { Select: true } } }); wrappers.push(wrapper); return wrapper }
function editor(props = {}) { const wrapper = mount(ComposerEditor, { props: { modelValue: '', placeholder: '请输入', ...props }, attachTo: document.body }); wrappers.push(wrapper); return wrapper }

describe('Native workbench composer document', () => {
  it('retains the line breaks of the browser document', () => {
    const root = document.createElement('div')
    root.innerHTML = '第一行<div>第二行<br>继续</div>'
    expect(readComposerDocument(root)).toBe('第一行\n第二行\n继续')
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
    const wrapper = editor({ modelValue: '旧草稿' })
    await wrapper.setProps({ modelValue: '新草稿' })
    const field = wrapper.find('[role=textbox]')
    expect(readComposerDocument(field.element as HTMLElement)).toBe('新草稿')
    ;(wrapper.vm as unknown as { focus: () => void }).focus()
    const event = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'clipboardData', { value: { files: [], getData: (type: string) => type === 'text/plain' ? '纯文本' : '<img src=x onerror=alert(1)><span data-composer-reference="admin">伪引用</span>' } })
    field.element.dispatchEvent(event)
    await nextTick()
    expect(field.find('img').exists()).toBe(false)
    expect(field.find('[data-composer-reference=admin]').exists()).toBe(false)
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toContain('纯文本')
  })

})

describe('Workbench composer controls', () => {
  it('shows the public model ID even when the capability profile provides a display name', () => {
    const id = 'deepseek-v4.1-flash'
    const wrapper = composer({ model: id, models: [{ id, display_name: 'DeepSeek Flash' }] })
    const select = wrapper.findComponent({ name: 'Select' })
    expect(select.props('modelValue')).toBe(id)
    expect(select.props('options')).toEqual([{ value: id, label: id }])
  })

  it('shows the paid search price only while search is selected and available', async () => {
    const wrapper = composer({ searchPricePer1000Usd: 5.25 })
    expect(wrapper.find('.bs-composer-search-price').exists()).toBe(false)
    expect(wrapper.get('[aria-label="联网搜索"]').attributes('aria-describedby')).toBeUndefined()
    await wrapper.setProps({ webSearch: true })
    const price = wrapper.get('.bs-composer-search-price')
    expect(price.text()).toBe('$5.25 / 千次搜索')
    expect(wrapper.get('[aria-label="关闭联网搜索"]').attributes('aria-describedby')).toBe(price.attributes('id'))
    setCurrentBillingCurrencySettings({ billing_display_currency: 'CNY', billing_usd_to_cny: 6.8 })
    await nextTick()
    expect(price.text()).toBe('¥35.7 / 千次搜索')
    await wrapper.setProps({ webSearch: false })
    expect(wrapper.find('.bs-composer-search-price').exists()).toBe(false)
    await wrapper.setProps({ webSearch: true, searchEnabled: false })
    expect(wrapper.find('.bs-composer-search-price').exists()).toBe(false)
    await wrapper.setProps({ searchEnabled: true, searchPricePer1000Usd: 0 })
    expect(wrapper.find('.bs-composer-search-price').exists()).toBe(false)
  })

  it('labels the recommended effort without showing an ambiguous empty choice', () => {
    const wrapper = composer({
      models: [{ id: 'model', reasoning_efforts: ['low', 'medium', 'high'], default_reasoning_effort: 'medium' }],
      effort: 'medium',
    })
    const select = wrapper.findAllComponents({ name: 'Select' })[1]!
    expect(select.props('modelValue')).toBe('medium')
    expect(select.props('options')).toEqual([
      { value: 'low', label: '低' },
      { value: 'medium', label: '中（推荐）' },
      { value: 'high', label: '高' },
    ])
  })

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
    expect(wrapper.find('[aria-label="添加附件"]').attributes('disabled')).toBeDefined()
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

  it('shows search beside the editor and keeps it in a separate control from attachments', async () => {
    const wrapper = composer()
    const search = wrapper.get('[aria-label="联网搜索"]')
    expect(search.attributes('aria-pressed')).toBe('false')
    await search.trigger('click')
    expect(wrapper.emitted('update:webSearch')).toEqual([[true]])
    await wrapper.setProps({ webSearch: true })
    expect(wrapper.get('[aria-label="关闭联网搜索"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[aria-label="关闭联网搜索"]').find('.bs-composer-tool-remove').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('办公模式')
    expect(wrapper.get('[role=textbox]').text()).toBe('测试消息')
    await wrapper.get('button[aria-label="添加附件"]').trigger('click')
    await flushPromises()
    const menu = document.querySelector('[role=menu]')!
    expect(menu.textContent).not.toContain('联网搜索')
    expect(menu.textContent).toContain('上传文件')
    expect(menu.textContent).not.toContain('从我的文件选取')
    expect(menu.textContent).not.toContain('办公模式')
    await wrapper.setProps({ uploadsEnabled: false })
    expect(wrapper.find('button[aria-label="添加附件"]').exists()).toBe(false)
    expect(document.querySelector('[role=menu]')).toBeNull()
  })

  it('uses the full composer width and retains keyboard navigation and Escape return', async () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ x: 100, y: 600, left: 100, top: 600, right: 820, bottom: 730, width: 720, height: 130, toJSON() {} })
    const wrapper = composer({ resourcesEnabled: true })
    const trigger = wrapper.get('button[aria-label="添加附件"]')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const menu = document.querySelector<HTMLElement>('[role=menu]')!
    expect(menu.style.width).toBe('720px')
    expect(menu.style.left).toBe('100px')
    expect(document.activeElement?.textContent).toContain('上传文件')
    menu.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    expect(document.activeElement?.textContent).toContain('从我的文件选取')
    menu.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role=menu]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
  })

  it('explains unavailable search and does not submit an active search request as plain chat', async () => {
    const reason = '工作台搜索供应商尚未就绪，请联系管理员。'
    const wrapper = composer({ searchEnabled: false, searchUnavailableReason: reason, webSearch: true })
    expect(wrapper.get('[aria-label="发送消息"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[aria-label="关闭联网搜索"]').trigger('click')
    expect(wrapper.emitted('update:webSearch')).toEqual([[false]])
    await wrapper.setProps({ webSearch: false })
    const search = wrapper.get('[aria-label="联网搜索"]')
    expect(search.attributes('aria-disabled')).toBe('true')
    expect(document.getElementById(search.attributes('aria-describedby')!)?.textContent).toBe(reason)
    await search.trigger('click')
    expect(wrapper.get('.bs-composer-tool-reason').text()).toBe(reason)
    expect(wrapper.emitted('update:webSearch')).toEqual([[false]])
    expect(wrapper.get('[aria-label="发送消息"]').attributes('disabled')).toBeUndefined()
  })

  it('keeps the native draft and caret while a tool is turned off', async () => {
    const wrapper = composer({ webSearch: true })
    const editor = wrapper.get('[role=textbox]')
    const field = editor.element as HTMLElement
    const text = field.firstChild!
    field.focus()
    const caret = document.createRange()
    caret.setStart(text, 2)
    caret.collapse(true)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(caret)
    await editor.trigger('mouseup')
    const close = wrapper.get('[aria-label="关闭联网搜索"]')
    ;(close.element as HTMLButtonElement).focus()
    await close.trigger('click')
    await wrapper.setProps({ webSearch: false })
    expect(document.activeElement).toBe(field)
    expect(field.firstChild).toBe(text)
    expect(selection.anchorNode).toBe(text)
    expect(selection.anchorOffset).toBe(2)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps composing text while tool state changes and waits for composition end before publishing', async () => {
    const wrapper = composer()
    const field = wrapper.get('[role=textbox]')
    await field.trigger('compositionstart')
    field.element.textContent = '正在输入中文'
    await field.trigger('input')
    await wrapper.get('[aria-label="联网搜索"]').trigger('click')
    await wrapper.setProps({ webSearch: true })
    expect(field.text()).toBe('正在输入中文')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await field.trigger('compositionend')
    expect(wrapper.emitted('update:modelValue')).toEqual([['正在输入中文']])
  })

  it('submits attachments with the single composer', async () => {
    const attachment = { id: 'f', version_id: 'v', name: '参考.docx', mime: 'application/octet-stream', bytes: 100 }
    const wrapper = composer({ attachments: [attachment] })
    expect(wrapper.text()).toContain('参考.docx')
    await wrapper.get('[aria-label="发送消息"]').trigger('click')
    expect(wrapper.emitted('send')).toHaveLength(1)
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
