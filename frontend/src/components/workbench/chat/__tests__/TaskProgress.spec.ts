import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TaskProgress from '../TaskProgress.vue'

vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: {} }))
vi.mock('@/stores/workbenchChat', () => ({ chatError: () => '请求失败', useWorkbenchChatStore: () => ({ loadRunEvents: vi.fn() }) }))

describe('TaskProgress terminal status', () => {
  it('shows cancellation even when a previous uncertain result retains its error details', () => {
    const wrapper = mount(TaskProgress, { props: {
      run: { id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'cancelled', created_at: '', updated_at: '', error_code: 'RECOVERY_CONFIRMATION_REQUIRED', error_message: 'Previous execution interrupted' },
      events: [],
    }, global: { stubs: { Icon: true, ClarifyCard: true } } })
    expect(wrapper.get('[role="status"]').text()).toBe('已停止本次任务')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('终止本次任务')
    wrapper.unmount()
  })
})
