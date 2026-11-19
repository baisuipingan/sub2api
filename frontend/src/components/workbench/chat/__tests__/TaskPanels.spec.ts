import { mount, enableAutoUnmount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import DeepStepGroup from '../DeepStepGroup.vue'
import PinnedTaskPanel from '../PinnedTaskPanel.vue'
import type { ExecutionEpisode } from '../taskPresentation'
import type { ChatEvent, ChatRun } from '@/types/workbenchChat'

enableAutoUnmount(afterEach)
const run: ChatRun = { id: 'run', conversation_id: 'conversation', model: 'model', status: 'running', created_at: '', updated_at: '' }
const events: ChatEvent[] = [{ id: 'plan', run_id: run.id, sequence: 1, type: 'task.updated', payload: { tasks: [{ id: 'task', title: '生成文档', status: 'completed' }] }, created_at: '' }]
const episode: ExecutionEpisode = { id: 'episode', active: true, steps: [{ id: 'operation', title: '读取参考资料', name: 'read_file', message: '', state: 'running', sequence: 1 }] }

describe('source task panels', () => {
  beforeEach(() => sessionStorage.clear())
  it('starts collapsed and preserves a manual fold through live events and conversation remounts', async () => {
    const wrapper = mount(DeepStepGroup, { props: { episode } })
    expect(wrapper.get('.bso-group-trigger').attributes('aria-expanded')).toBe('false')
    await wrapper.get('.bso-group-trigger').trigger('click')
    await wrapper.setProps({ episode: { ...episode, steps: [{ ...episode.steps[0]!, state: 'completed' }] } })
    expect(wrapper.get('.bso-group-trigger').attributes('aria-expanded')).toBe('true')
    wrapper.unmount()
    const reopened = mount(DeepStepGroup, { props: { episode: { ...episode, active: false } } })
    expect(reopened.get('.bso-group-trigger').attributes('aria-expanded')).toBe('true')
    await reopened.setProps({ episode: { ...episode, steps: [{ ...episode.steps[0]!, id: 'other-operation' }] } })
    expect(reopened.get('.bso-group-trigger').attributes('aria-expanded')).toBe('false')
  })

  it('keeps the control usable when session storage is blocked', async () => {
    const spy = Object.getOwnPropertyDescriptor(window, 'sessionStorage')!
    Object.defineProperty(window, 'sessionStorage', { configurable: true, get: () => { throw new Error('blocked') } })
    try {
      const wrapper = mount(DeepStepGroup, { props: { episode } })
      await wrapper.get('.bso-group-trigger').trigger('click')
      expect(wrapper.get('.bso-group-trigger').attributes('aria-expanded')).toBe('true')
    } finally { Object.defineProperty(window, 'sessionStorage', spy) }
  })

  it('calls a finished checklist wrap-up until the run itself finishes', async () => {
    const wrapper = mount(PinnedTaskPanel, { props: { run, events } })
    expect(wrapper.text()).toContain('正在整理成果')
    expect(wrapper.text()).not.toContain('任务已完成')
    expect(wrapper.find('.bs-chat-spin').exists()).toBe(true)
    await wrapper.get('.bso-plan-trigger').trigger('click')
    await wrapper.setProps({ run: { ...run, status: 'waiting_user' } })
    expect(wrapper.text()).toContain('等待补充信息')
    expect(wrapper.find('.bs-chat-spin').exists()).toBe(false)
    await wrapper.setProps({ run: { ...run, id: 'another-run', status: 'failed' } })
    expect(wrapper.get('.bso-plan-trigger').attributes('aria-expanded')).toBe('false')
    expect(wrapper.text()).toContain('任务未完成')
  })
})
