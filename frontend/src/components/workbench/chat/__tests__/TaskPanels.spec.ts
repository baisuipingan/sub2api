import { mount, enableAutoUnmount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import DeepStepGroup from '../DeepStepGroup.vue'
import type { ExecutionEpisode } from '../taskPresentation'

enableAutoUnmount(afterEach)
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

})
