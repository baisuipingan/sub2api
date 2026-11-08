import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh'
import WorkbenchTaskRoutes from '../WorkbenchTaskRoutes.vue'
import type { WorkbenchTaskRouteCall } from '@/api/admin/workbench'

function render(calls: WorkbenchTaskRouteCall[]) {
  return mount(WorkbenchTaskRoutes, { props: { calls }, global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: { zh } })] } })
}

describe('observed workbench upstream routes', () => {
  it('shows the actual mapped success model and all retained retry records per child', () => {
    const wrapper = render([
      { ordinal: 0, state: 'recorded', upstreams: [
        { account_id: 11, account_name: 'first supplier', upstream_model: null, outcome: 'failed', status_code: 503, recorded_at: '2026-11-08T00:00:00Z' },
        { account_id: 12, account_name: 'final supplier', upstream_model: 'private-2k-model', outcome: 'usage', recorded_at: '2026-11-08T00:00:01Z' },
      ] },
      { ordinal: 1, state: 'pending', upstreams: [] },
    ])
    expect(wrapper.text()).toContain('最终记录用量的上游')
    expect(wrapper.text()).toContain('尝试 1')
    expect(wrapper.text()).toContain('尝试 2')
    expect(wrapper.text()).toContain('final supplier')
    expect(wrapper.text()).toContain('#12')
    expect(wrapper.text()).toContain('private-2k-model')
    expect(wrapper.text()).toContain('first supplier')
    expect(wrapper.text()).toContain('HTTP 503')
    expect(wrapper.text()).toContain('模型未留痕')
    expect(wrapper.text()).toContain('生成请求 1')
    expect(wrapper.text()).toContain('生成请求 2')
    expect(wrapper.find('summary').text()).toContain('2 次生成请求 · 2 次上游尝试')
    expect(wrapper.text()).toContain('部分生成请求尚无上游记录')
    expect(wrapper.findAll('button')).toHaveLength(0)
    wrapper.unmount()
  })

  it.each([
    ['not_dispatched', '未调度'],
    ['pending', '上游记录待返回'],
    ['unknown', '实际上游未知（无留痕）'],
  ] as const)('distinguishes %s without inventing a route', (state, label) => {
    const wrapper = render([{ ordinal: 0, state, upstreams: [] }])
    expect(wrapper.text()).toBe(label)
    expect(wrapper.find('details').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps missing legacy API metadata unknown', () => {
    const wrapper = render([])
    expect(wrapper.text()).toContain('实际上游未知（无留痕）')
    wrapper.unmount()
  })
})
