import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ChatMarkdown from '../ChatMarkdown.vue'
const file = { id: 'file', version_id: 'version', name: '季度汇报.docx', mime: 'application/octet-stream', bytes: 1 }
describe('workbench markdown', () => {
  it('sanitizes active model HTML and keeps unsafe URLs inert', async () => { const open = vi.spyOn(window, 'open').mockImplementation(() => null); const wrapper = mount(ChatMarkdown, { props: { content: '<img src="x" onerror="alert(1)"><iframe src="https://example.test"></iframe>[危险](javascript:alert%281%29)' } }); expect(wrapper.html()).not.toContain('onerror'); expect(wrapper.find('iframe').exists()).toBe(false); await wrapper.find('a').trigger('click'); expect(open).not.toHaveBeenCalled(); open.mockRestore() })
  it('resolves a generated local artifact link into the authorized file preview', async () => { const wrapper = mount(ChatMarkdown, { props: { content: '[季度汇报](sandbox:/mnt/data/output/季度汇报.docx)', files: [file] } }); expect(wrapper.get('a').attributes('href')).toBe('#workbench-file-version'); await wrapper.get('a').trigger('click'); expect(wrapper.emitted('preview')?.[0]).toEqual([file]) })
  it('resolves absolute output paths only when an authorized artifact matches', async () => {
    const wrapper = mount(ChatMarkdown, { props: { content: '[季度汇报](/output/季度汇报.docx) [未知文件](/output/other.docx)', files: [file] } })
    const links = wrapper.findAll('a')
    expect(links[0]!.attributes('href')).toBe('#workbench-file-version')
    expect(links[1]!.attributes('href')).toBe('/output/other.docx')
    await links[0]!.trigger('click'); await links[1]!.trigger('click')
    expect(wrapper.emitted('preview')).toEqual([[file]])
  })
  it('keeps an external link external even when its file name matches a deliverable', () => { const wrapper = mount(ChatMarkdown, { props: { content: '[原始资料](https://example.test/季度汇报.docx)', files: [file] } }); expect(wrapper.get('a').attributes('href')).toContain('https://example.test/'); expect(wrapper.emitted('preview')).toBeUndefined() })
})
