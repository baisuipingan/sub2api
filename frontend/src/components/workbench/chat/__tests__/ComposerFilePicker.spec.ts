import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ComposerFilePicker from '../ComposerFilePicker.vue'
import { workbenchChatApi } from '@/api/workbenchChat'
import type { ChatArtifact } from '@/types/workbenchChat'

vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: { resources: vi.fn(), access: vi.fn(), attachGalleryImage: vi.fn() } }))
const first: ChatArtifact = { id: 'resource-1', version_id: 'version-1', name: '说明.docx', mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', bytes: 1024 }
const second: ChatArtifact = { id: 'resource-2', version_id: 'version-2', name: '图片.png', mime: 'image/png', bytes: 2000 }
const third: ChatArtifact = { id: 'resource-3', version_id: 'version-3', name: '数据.xlsx', mime: 'application/octet-stream', bytes: 3000 }
let wrapper: ReturnType<typeof mount> | undefined
const global = { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }, SearchInput: { props: ['modelValue'], emits: ['update:modelValue'], template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' } } }
beforeEach(() => { vi.mocked(workbenchChatApi.resources).mockResolvedValue({ items: [first, second, third] }) })
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.clearAllMocks() })

describe('Authorized composer file selection', () => {
  it('lists metadata without signing/fetching image objects, counts existing files and emits only new additions', async () => {
    wrapper = mount(ComposerFilePicker, { props: { show: true, attachments: [first], maxFiles: 2 }, global })
    await flushPromises()
    const checks = wrapper.findAll<HTMLInputElement>('input[type=checkbox]')
    expect(checks[0]!.element.checked).toBe(true)
    expect(checks[0]!.element.disabled).toBe(true)
    await checks[1]!.setValue(true)
    expect(checks[2]!.element.disabled).toBe(true)
    expect(wrapper.text()).toContain('2 / 2')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(workbenchChatApi.access).not.toHaveBeenCalled()
    await wrapper.find('.btn-primary').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[[second]]])
  })

  it('deduplicates paged versions and filters file names while preserving selected items', async () => {
    vi.mocked(workbenchChatApi.resources).mockResolvedValueOnce({ items: [first, second], next_cursor: 'next' }).mockResolvedValueOnce({ items: [second, third] })
    wrapper = mount(ComposerFilePicker, { props: { show: true, attachments: [] }, global })
    await flushPromises()
    await wrapper.find('.bs-composer-picker-more').trigger('click')
    await flushPromises()
    expect(workbenchChatApi.resources).toHaveBeenLastCalledWith('next')
    expect(wrapper.findAll('input[type=checkbox]')).toHaveLength(3)
    await wrapper.findAll('input[type=checkbox]')[0]!.setValue(true)
    await wrapper.find('input:not([type=checkbox])').setValue('图片')
    expect(wrapper.findAll('input[type=checkbox]')).toHaveLength(1)
    await wrapper.find('.btn-primary').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[[first]]])
  })

  it('ignores an old request after closing and reopening the picker', async () => {
    let finish: (value: { items: ChatArtifact[] }) => void = () => {}
    vi.mocked(workbenchChatApi.resources).mockReturnValueOnce(new Promise(resolve => { finish = resolve })).mockResolvedValueOnce({ items: [third] })
    wrapper = mount(ComposerFilePicker, { props: { show: true, attachments: [] }, global })
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    finish({ items: [first] })
    await flushPromises()
    expect(wrapper.text()).toContain('数据.xlsx')
    expect(wrapper.text()).not.toContain('说明.docx')
  })

  it('adopts only a confirmed gallery selection', async () => {
    const image = { ...second, id: 'studio-image', source: 'gallery' as const }
    vi.mocked(workbenchChatApi.resources).mockResolvedValueOnce({ items: [first] }).mockResolvedValueOnce({ items: [image] })
    vi.mocked(workbenchChatApi.attachGalleryImage).mockResolvedValue({ ...image, id: 'office-image' })
    wrapper = mount(ComposerFilePicker, { props: { show: true, attachments: [], attachmentExtensions: ['.docx', '.png'] }, global })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '云端图库')!.trigger('click')
    await flushPromises()
    await wrapper.get<HTMLInputElement>('input[type=checkbox]').setValue(true)
    expect(workbenchChatApi.resources).toHaveBeenLastCalledWith(undefined, 'gallery')
    expect(workbenchChatApi.attachGalleryImage).not.toHaveBeenCalled()
    await wrapper.get('.btn-primary').trigger('click')
    await flushPromises()
    expect(workbenchChatApi.attachGalleryImage).toHaveBeenCalledWith('studio-image')
    expect(wrapper.emitted('select')).toEqual([[[{ ...image, id: 'office-image' }]]])
  })
})
