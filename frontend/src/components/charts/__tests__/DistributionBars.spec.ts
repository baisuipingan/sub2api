import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import DistributionBars from '../DistributionBars.vue'
import type { UserBreakdownItem } from '@/types'

const { getUserBreakdown } = vi.hoisted(() => ({ getUserBreakdown: vi.fn() }))
vi.mock('@/api/admin/dashboard', () => ({ getUserBreakdown }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const modelStats = [
  { model: 'model-a', requests: 10, total_tokens: 2000, actual_cost: 1, cost: 2 },
  { model: 'model-b', requests: 20, total_tokens: 1000, actual_cost: 2, cost: 3 },
]
const user: UserBreakdownItem = {
  user_id: 7, email: 'user@example.com', requests: 10,
  input_tokens: 500, output_tokens: 500, cache_tokens: 1000, total_tokens: 2000,
  actual_cost: 1, account_cost: 0.5, cost: 2,
}
const wrappers: VueWrapper[] = []
const render = (props: Record<string, unknown> = {}) => {
  const wrapper = mount(DistributionBars, {
    props: { modelStats, ...props },
    global: { stubs: { LoadingSpinner: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

beforeEach(() => {
  getUserBreakdown.mockReset().mockResolvedValue({ users: [user] })
})
afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
})

describe('DistributionBars user breakdown', () => {
  it('keeps user-page bars read-only and formats Token counts without currency', async () => {
    const wrapper = render()
    expect(wrapper.findAll('button.cbar')).toHaveLength(0)
    expect(wrapper.findAll('.cbar-val')[0].text()).toBe('2.0K')
    expect(wrapper.findAll('.cbar-val')[0].text()).not.toMatch(/[$¥]/)
    await wrapper.find('.cbar').trigger('click')
    expect(getUserBreakdown).not.toHaveBeenCalled()
  })

  it('loads the selected model lazily with the full chart scope and labeled user metrics', async () => {
    const wrapper = render({
      enableBreakdown: true,
      startDate: '2026-11-11T14:00:00', endDate: '2026-11-12T14:00:00',
      filters: { user_id: 7, api_key_id: 8, account_id: 9, group_id: 10, request_type: 'stream',
        stream: true, native_compaction_v2: false, billing_type: 0, upstream_model_mismatch: false },
    })
    expect(getUserBreakdown).not.toHaveBeenCalled()
    await wrapper.findAll('button.cbar')[0].trigger('click')
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenCalledWith({
      start_date: '2026-11-11T14:00:00', end_date: '2026-11-12T14:00:00',
      model: 'model-a', model_source: 'requested', sort_by: 'total_tokens', limit: 50,
      user_id: 7, api_key_id: 8, account_id: 9, group_id: 10, request_type: 'stream',
      stream: true, native_compaction_v2: false, billing_type: 0, upstream_model_mismatch: false,
    }, { signal: expect.any(AbortSignal) })
    expect(wrapper.find('button.cbar').attributes('aria-expanded')).toBe('true')
    expect(wrapper.find('[role="region"]').attributes('id')).toBe(wrapper.find('button.cbar').attributes('aria-controls'))
    expect(wrapper.find('table').text()).toContain('user@example.com')
    expect(wrapper.findAll('thead th')).toHaveLength(6)
    expect(wrapper.findAll('tbody td')).toHaveLength(6)
  })

  it.each(['requested', 'upstream', 'mapping'] as const)('preserves the complete model identity for %s', async source => {
    const name = source === 'mapping' ? 'alias -> actual-upstream' : 'actual-upstream'
    const stats = [{ ...modelStats[0], model: name }]
    const wrapper = render({ enableBreakdown: true, modelSource: source,
      modelStats: stats, upstreamModelStats: stats, mappingModelStats: stats })
    await wrapper.find('button.cbar').trigger('click')
    expect(getUserBreakdown).toHaveBeenCalledWith(expect.objectContaining({ model: name, model_source: source }), expect.anything())
  })

  it('identifies same-name groups by ID and leaves the ungrouped row unexpandable', async () => {
    const wrapper = render({ enableBreakdown: true, defaultTab: 'group', groupStats: [
      { ...modelStats[0], group_id: 11, group_name: 'same' },
      { ...modelStats[1], group_id: 12, group_name: 'same' },
      { ...modelStats[1], group_id: 0, group_name: 'ungrouped' },
    ] })
    expect(wrapper.findAll('button.cbar')).toHaveLength(2)
    await wrapper.findAll('button.cbar')[0].trigger('click')
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenLastCalledWith(expect.objectContaining({ group_id: 11 }), expect.anything())
    await wrapper.findAll('button.cbar')[1].trigger('click')
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenLastCalledWith(expect.objectContaining({ group_id: 12 }), expect.anything())
    expect(wrapper.findAll('button.cbar')[0].attributes('aria-expanded')).toBe('false')
    expect(wrapper.findAll('button.cbar')[1].attributes('aria-expanded')).toBe('true')
  })

  it.each(['inbound', 'upstream', 'path'] as const)('drills down on the %s endpoint', async source => {
    const stats = [{ ...modelStats[0], endpoint: '/v1/responses' }]
    const wrapper = render({ enableBreakdown: true, defaultTab: 'endpoint', endpointSource: source,
      endpointStats: stats, upstreamEndpointStats: stats, endpointPathStats: stats })
    await wrapper.find('button.cbar').trigger('click')
    expect(getUserBreakdown).toHaveBeenCalledWith(expect.objectContaining({ endpoint: '/v1/responses', endpoint_type: source }), expect.anything())
  })

  it('sorts the user query by the selected cost metric', async () => {
    const wrapper = render({ enableBreakdown: true, metric: 'actual_cost' })
    await wrapper.find('button.cbar').trigger('click')
    expect(getUserBreakdown).toHaveBeenCalledWith(expect.objectContaining({ model: 'model-b', sort_by: 'actual_cost' }), expect.anything())
  })

  it('keeps the endpoint model and billing scope without narrowing the other distribution tabs', async () => {
    const wrapper = render({ enableBreakdown: true, defaultTab: 'endpoint',
      endpointStats: [{ ...modelStats[0], endpoint: '/v1/images/generations' }],
      filters: { user_id: 7 }, endpointFilters: { model: 'image-model', billing_mode: 'image' },
    })
    await wrapper.find('button.cbar').trigger('click')
    expect(getUserBreakdown).toHaveBeenLastCalledWith(expect.objectContaining({
      user_id: 7, endpoint: '/v1/images/generations', endpoint_type: 'inbound',
      model: 'image-model', billing_mode: 'image',
    }), expect.anything())
    await wrapper.findAll('[role="tab"]')[0].trigger('click')
    await wrapper.find('button.cbar').trigger('click')
    const modelParams = getUserBreakdown.mock.lastCall![0]
    expect(modelParams).toMatchObject({ user_id: 7, model: 'model-a', model_source: 'requested' })
    expect(modelParams).not.toHaveProperty('billing_mode')
  })

  it('ignores a late response and rejection from an earlier row', async () => {
    const first = deferred<{ users: UserBreakdownItem[] }>()
    const second = deferred<{ users: UserBreakdownItem[] }>()
    getUserBreakdown.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = render({ enableBreakdown: true })
    await wrapper.findAll('button.cbar')[0].trigger('click')
    const signal = getUserBreakdown.mock.calls[0][1].signal as AbortSignal
    await wrapper.findAll('button.cbar')[1].trigger('click')
    expect(signal.aborted).toBe(true)
    second.resolve({ users: [{ ...user, email: 'second@example.com' }] })
    await flushPromises()
    first.reject(new Error('late cancellation'))
    await flushPromises()
    expect(wrapper.find('[role="region"]').text()).toContain('second@example.com')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it.each([
    { modelSource: 'upstream' }, { startDate: '2026-11-01' }, { endDate: '2026-11-02' },
    { filters: { account_id: 17 } }, { breakdownRefreshKey: 1 }, { metric: 'actual_cost' }, { loading: true },
    { endpointFilters: { model: 'new-model' } },
    { enableBreakdown: false },
  ])('invalidates an in-flight breakdown when its context changes: %j', async props => {
    const pending = deferred<{ users: UserBreakdownItem[] }>()
    getUserBreakdown.mockReturnValueOnce(pending.promise)
    const wrapper = render({ enableBreakdown: true })
    await wrapper.find('button.cbar').trigger('click')
    const signal = getUserBreakdown.mock.calls[0][1].signal as AbortSignal
    await wrapper.setProps(props)
    expect(signal.aborted).toBe(true)
    pending.resolve({ users: [user] })
    await flushPromises()
    expect(wrapper.find('[role="region"]').exists()).toBe(false)
  })

  it('cancels on collapse and refetches on re-open without reusing stale users', async () => {
    const pending = deferred<{ users: UserBreakdownItem[] }>()
    getUserBreakdown.mockReturnValueOnce(pending.promise)
    const wrapper = render({ enableBreakdown: true })
    await wrapper.find('button.cbar').trigger('click')
    await wrapper.find('button.cbar').trigger('click')
    expect((getUserBreakdown.mock.calls[0][1].signal as AbortSignal).aborted).toBe(true)
    pending.resolve({ users: [{ ...user, email: 'stale@example.com' }] })
    await flushPromises()
    expect(wrapper.find('[role="region"]').exists()).toBe(false)
    await wrapper.find('button.cbar').trigger('click')
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenCalledTimes(2)
    expect(wrapper.find('table').text()).not.toContain('stale@example.com')
  })

  it('shows a retryable error instead of treating a failure as empty usage', async () => {
    getUserBreakdown.mockRejectedValueOnce(new Error('network error'))
    const wrapper = render({ enableBreakdown: true })
    await wrapper.find('button.cbar').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('usage.distribution.breakdownFailed')
    expect(wrapper.find('table').exists()).toBe(false)
    await wrapper.find('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenCalledTimes(2)
    expect(wrapper.find('table').text()).toContain(user.email)
  })

  it('closes the old breakdown when switching tabs', async () => {
    const wrapper = render({ enableBreakdown: true })
    await wrapper.find('button.cbar').trigger('click')
    await flushPromises()
    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    expect(wrapper.find('[role="region"]').exists()).toBe(false)
  })
})
