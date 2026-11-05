import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import Toggle from '@/components/common/Toggle.vue'
import StudioCheckbox from '../StudioCheckbox.vue'

enableAutoUnmount(afterEach)

describe('StudioCheckbox compatibility wrapper', () => {
  it('uses the shared Toggle and retains the existing change event contract', async () => {
    const wrapper = mount(StudioCheckbox, { props: { checked: false, label: 'Keep local history' } })
    expect(wrapper.findComponent(Toggle).exists()).toBe(true)
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    const control = wrapper.get('[role="switch"]')
    expect(control.attributes('aria-label')).toBe('Keep local history')
    await control.trigger('click')
    expect(wrapper.emitted('change')).toEqual([[true]])
  })
})
