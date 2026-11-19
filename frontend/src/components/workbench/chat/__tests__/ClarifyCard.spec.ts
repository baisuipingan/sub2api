import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import ClarifyCard from '../ClarifyCard.vue'
const request = { request_id: 'q', questions: [{ id: 'first', question: '需要什么格式？', options: ['Word', 'PPT'] }, { id: 'second', question: '包含哪些内容？', options: ['图表', '摘要'], multiple: true }] }
describe('Bisheng clarification interaction', () => {
  it('advances a single choice, collects multiple choices, and submits all answers once', async () => {
    const wrapper = mount(ClarifyCard, { props: { request } })
    await wrapper.findAll('.bs-chat-clarify-options button')[0]!.trigger('click')
    expect(wrapper.text()).toContain('包含哪些内容')
    expect(wrapper.emitted('submit')).toBeUndefined()
    await wrapper.findAll('.bs-chat-clarify-options button')[0]!.trigger('click')
    await wrapper.findAll('.bs-chat-clarify-options button')[1]!.trigger('click')
    await wrapper.find('footer button').trigger('click')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toEqual([{ question_id: 'first', values: ['Word'] }, { question_id: 'second', values: ['图表', '摘要'] }])
  })
  it('does not preselect or invent answers when the user closes the questions', async () => {
    const wrapper = mount(ClarifyCard, { props: { request } })
    await wrapper.find('header button').trigger('click')
    expect(wrapper.emitted('submit')?.[0]?.[0]).toEqual([{ question_id: 'first', values: [] }, { question_id: 'second', values: [] }])
  })
})
