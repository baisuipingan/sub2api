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
    expect(links[1]!.attributes('href')).toBeUndefined()
    await links[0]!.trigger('click'); await links[1]!.trigger('click')
    expect(wrapper.emitted('preview')).toEqual([[file]])
  })
  it('keeps an external link external even when its file name matches a deliverable', () => { const wrapper = mount(ChatMarkdown, { props: { content: '[原始资料](https://example.test/季度汇报.docx)', files: [file] } }); expect(wrapper.get('a').attributes('href')).toContain('https://example.test/'); expect(wrapper.emitted('preview')).toBeUndefined() })
  it('only turns authorized citation links into source buttons', async () => {
    const citation = { id: '1', title: '官方资料', url: 'https://example.test/docs' }
    const wrapper = mount(ChatMarkdown, { props: { content: '[1](https://example.test/docs) [2](https://other.test/) [3](#workbench-source-fake)', citations: [citation] } })
    const links = wrapper.findAll('a')
    expect(links[0]!.classes()).toContain('bs-chat-inline-citation')
    expect(links[1]!.classes()).not.toContain('bs-chat-inline-citation')
    await links[0]!.trigger('click'); await links[2]!.trigger('click')
    expect(wrapper.emitted('reference')).toEqual([[citation]])
  })
  it('removes automatic remote media and leaves no unsafe href for middle-click navigation', () => {
    const wrapper = mount(ChatMarkdown, { props: { content: '[FTP](ftp://example.test/) [凭据](https://user:secret@example.test/) [相对链接](//other.test/) [邮箱](mailto:a@example.test) [安全](https://example.test/) ![跟踪](https://other.test/pixel.png)<video src="https://other.test/video"></video>' } })
    const links = wrapper.findAll('a')
    expect(links.slice(0, 4).every(link => link.attributes('href') === undefined)).toBe(true)
    expect(links[4]!.attributes()).toMatchObject({ href: 'https://example.test/', target: '_blank', rel: 'noopener noreferrer', referrerpolicy: 'no-referrer' })
    expect(wrapper.find('img, video, audio, iframe, source').exists()).toBe(false)
  })
  it('builds code copy controls after sanitizing model-supplied buttons', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const wrapper = mount(ChatMarkdown, { props: { content: '<button data-copy-code>假的</button>\n\n```js\nconst 中文 = "ok"\n```' } })
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.get('.bs-chat-code-toolbar span').text()).toBe('js')
    await wrapper.get('button').trigger('click')
    expect(writeText).toHaveBeenCalledWith('const 中文 = "ok"\n')
    expect(wrapper.get('button').text()).toBe('已复制')
    vi.unstubAllGlobals()
  })
})
