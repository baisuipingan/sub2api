import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Input from '@/components/common/Input.vue'
import StudioRatioPickerModal from '../StudioRatioPickerModal.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

function mountModal(overrides = {}) {
  return mount(StudioRatioPickerModal, {
    props: { currentRatio: '1:1', ratios: ['1:1', '21:9'], allPresetRatios: ['1:1', '21:9'], allowCustom: true, ...overrides },
    global: { stubs: { Teleport: true, Transition: false } },
  })
}

describe('StudioRatioPickerModal common controls', () => {
  it('uses BaseDialog and applies a selected preset with shared button styles', async () => {
    const wrapper = mountModal()
    expect(wrapper.findComponent(BaseDialog).exists()).toBe(true)
    await wrapper.findAll('button').find((button) => button.text() === '21:9')!.trigger('click')
    const apply = wrapper.findAll('button').find((button) => button.text() === 'imageStudio.ratio.apply')!
    expect(apply.classes()).toContain('btn-primary')
    await apply.trigger('click')

    expect(wrapper.emitted('select')).toEqual([['21:9']])
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('uses common Input and validates a custom ratio before applying', async () => {
    const wrapper = mountModal()
    await wrapper.findAll('button').find((button) => button.text() === 'imageStudio.ratio.custom')!.trigger('click')
    expect(wrapper.findComponent(Input).exists()).toBe(true)
    const input = wrapper.get('input')
    await input.setValue('invalid')
    const apply = wrapper.findAll('button').find((button) => button.text() === 'imageStudio.ratio.apply')!
    expect(apply.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('imageStudio.ratio.invalid')
    await input.setValue('5:4')
    await input.trigger('keyup', { key: 'Enter' })

    expect(wrapper.emitted('select')).toEqual([['5:4']])
  })

  it('does not show custom input when the model only permits presets', async () => {
    const wrapper = mountModal({ allowCustom: false })
    expect(wrapper.findComponent(Input).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('imageStudio.ratio.custom')
  })

  it('closes on the common dialog backdrop and the studio Escape stack', async () => {
    const wrapper = mountModal()
    await wrapper.get('.modal-overlay').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(2)
    expect(wrapper.emitted('select')).toBeUndefined()
  })
})
