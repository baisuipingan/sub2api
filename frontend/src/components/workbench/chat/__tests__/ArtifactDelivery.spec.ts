import { mount, enableAutoUnmount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import WorkspaceFileList from '../WorkspaceFileList.vue'
import ResultSection from '../ResultSection.vue'
import type { ChatArtifact, ChatCitation } from '@/types/workbenchChat'

enableAutoUnmount(afterEach)
const file = (id: string, extra: Partial<ChatArtifact> = {}): ChatArtifact => ({ id, version_id: id, name: `${id}.docx`, mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', bytes: 1536, ...extra })

describe('workspace and final delivery', () => {
  it('uses the input/output role to group edited uploads and preserves every file operation', async () => {
    const files = [file('input', { source: 'upload', role: 'input' }), file('edited', { source: 'upload', role: 'output' }), file('generated', { source: 'generated' })]
    const wrapper = mount(WorkspaceFileList, { props: { files } })
    const groups = wrapper.findAll('.bso-file-group')
    expect(groups[0]!.text()).toContain('参考资料')
    expect(groups[0]!.findAll('.bso-workspace-file-row')).toHaveLength(1)
    expect(groups[1]!.text()).toContain('生成成果')
    expect(groups[1]!.findAll('.bso-workspace-file-row')).toHaveLength(2)
    await groups[1]!.get('.bso-file-open').trigger('click')
    await groups[1]!.get('.bso-file-save').trigger('click')
    expect(wrapper.emitted('preview')?.[0]).toEqual([files[1]])
    expect(wrapper.emitted('download')?.[0]).toEqual([files[1]])
  })

  it('distinguishes a loading or failed resource list from an empty workspace', async () => {
    const wrapper = mount(WorkspaceFileList, { props: { files: [], loading: true } })
    expect(wrapper.get('[role="status"]').attributes('aria-label')).toBe('正在加载文件')
    expect(wrapper.text()).not.toContain('还没有文件')
    await wrapper.setProps({ loading: false, loadFailed: true })
    expect(wrapper.text()).toContain('文件列表加载失败')
    expect(wrapper.text()).not.toContain('还没有文件')
    await wrapper.setProps({ loadFailed: false })
    expect(wrapper.text()).toContain('还没有文件')
  })

  it('hands off a sole deliverable with an always available download and forwards authorized citations', async () => {
    const files = [file('report')]
    const citation: ChatCitation = { id: 'source', title: '官方资料', url: 'https://example.org/reference' }
    const Markdown = defineComponent({ props: ['content', 'files', 'citations'], emits: ['reference'], template: '<button @click="$emit(\'reference\', citations[0])">资料</button>' })
    const wrapper = mount(ResultSection, { props: { text: '结论', files, citations: [citation] }, global: { stubs: { ChatMarkdown: Markdown } } })
    expect(wrapper.find('.bs-chat-result-files').exists()).toBe(false)
    await wrapper.get('button[aria-label="下载文件"]').trigger('click')
    expect(wrapper.emitted('download')?.[0]).toEqual([files[0]])
    await wrapper.getComponent(Markdown).trigger('click')
    expect(wrapper.getComponent(Markdown).props('citations')).toEqual([citation])
    expect(wrapper.emitted('reference')?.[0]).toEqual([citation])
  })

  it('shows the primary name, total count and individually downloadable complete manifest', async () => {
    const files = [file('report'), file('slides', { name: 'slides.pptx' }), file('sheet', { name: 'sheet.xlsx' })]
    const wrapper = mount(ResultSection, { props: { text: '', files } })
    expect(wrapper.get('.bs-chat-report-ready').text()).toContain('report.docx')
    expect(wrapper.get('.bs-chat-report-ready').text()).toContain('等 3 个文件')
    const rows = wrapper.findAll('.bs-chat-file-row')
    expect(rows).toHaveLength(3)
    await rows[2]!.get('.bs-chat-text-button').trigger('click')
    expect(wrapper.emitted('download')?.[0]).toEqual([files[2]])
  })
})
