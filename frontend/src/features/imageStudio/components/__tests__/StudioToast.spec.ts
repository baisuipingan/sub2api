import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { showToast } = vi.hoisted(() => ({ showToast: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showToast }) }))
vi.mock('../../stores/store', async () => ({ storeState: (await import('vue')).reactive({ toast: null as null | { type: 'error' | 'success'; message: string } }) }))

import StudioToast from '../StudioToast.vue'
import { storeState as state } from '../../stores/store'
enableAutoUnmount(afterEach)

describe('studio notifications use the app toast renderer', () => {
  it('forwards each notification once and does not render a duplicate overlay', async () => {
    showToast.mockClear()
    state.toast = null
    const wrapper = mount(StudioToast)
    state.toast = { type: 'error', message: '4K unavailable' }
    await nextTick()
    expect(showToast).toHaveBeenCalledWith('error', '4K unavailable', 6000)
    state.toast = { type: 'success', message: 'Ready' }
    await nextTick()
    expect(showToast).toHaveBeenCalledTimes(2)
    expect(showToast).toHaveBeenLastCalledWith('success', 'Ready', 3000)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
