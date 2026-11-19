import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ChatReferencesPanel from '../ChatReferencesPanel.vue'

describe('authorized source panel', () => {
  it('keeps source metadata inert, deduplicates IDs and only renders safe external links', () => {
    const wrapper = mount(ChatReferencesPanel, { props: { citations: [
      { id: '1', title: '<script>资料</script>', url: 'https://example.test/docs', excerpt: '<img src="https://evil.test/pixel">' },
      { id: '1', title: '正式资料', url: 'https://example.test/final' },
      { id: '2', title: '危险', url: 'javascript:alert(1)' },
      { id: '3', title: '含凭据', url: 'https://name:secret@example.test/' },
    ] } })
    expect(wrapper.findAll('a')).toHaveLength(1)
    expect(wrapper.get('a').attributes()).toMatchObject({ href: 'https://example.test/final', rel: 'noopener noreferrer', referrerpolicy: 'no-referrer' })
    expect(wrapper.find('script, img').exists()).toBe(false)
  })
  it('locates the requested source after opening and emits close', async () => {
    const scrollIntoView = vi.fn()
    const previous = HTMLElement.prototype.scrollIntoView
    HTMLElement.prototype.scrollIntoView = scrollIntoView
    const wrapper = mount(ChatReferencesPanel, { props: { citations: [{ id: 'second', title: '第二条', url: 'https://example.test' }] } })
    await wrapper.setProps({ activeId: 'second' })
    await flushPromises()
    expect(wrapper.get('[data-source-id="second"]').classes()).toContain('active')
    expect(scrollIntoView).toHaveBeenCalledWith({ block: 'nearest', behavior: 'smooth' })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
    HTMLElement.prototype.scrollIntoView = previous
  })
})
