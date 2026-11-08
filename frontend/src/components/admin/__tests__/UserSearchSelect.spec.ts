import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UserSearchSelect from '../UserSearchSelect.vue'
import { searchUsers } from '@/api/admin/usage'
import { getById } from '@/api/admin/users'

vi.mock('@/api/admin/usage', () => ({ searchUsers: vi.fn() }))
vi.mock('@/api/admin/users', () => ({ getById: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

const alice = { id: 7, username: 'Alice', email: 'alice@example.test', deleted: false }
const bob = { id: 8, username: 'Bob', email: 'bob@example.test', deleted: false }
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
function render(value?: number) {
  const wrapper = mount(UserSearchSelect, { props: {
    modelValue: value,
    id: 'user-filter',
    label: 'User',
    placeholder: 'Name, email or nickname',
    'onUpdate:modelValue': (next: number | undefined) => { void wrapper.setProps({ modelValue: next }) },
  } })
  return wrapper
}
async function type(wrapper: ReturnType<typeof render>, query: string) {
  await wrapper.get('input').setValue(query)
  await vi.advanceTimersByTimeAsync(300)
  await flushPromises()
}

describe('administrator user search reused from usage records', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(searchUsers).mockReset().mockResolvedValue([])
    vi.mocked(getById).mockReset().mockResolvedValue(alice as Awaited<ReturnType<typeof getById>>)
  })
  afterEach(() => { vi.useRealTimers() })

  it('uses the existing keyword endpoint after 300 ms and selects the exact matching user', async () => {
    vi.mocked(searchUsers).mockResolvedValue([alice])
    const wrapper = render()
    await wrapper.get('input').setValue('Alice')
    await vi.advanceTimersByTimeAsync(299)
    expect(searchUsers).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()
    expect(searchUsers).toHaveBeenLastCalledWith('Alice')
    expect(wrapper.get('[role="option"]').text()).toContain('Alice')
    expect(wrapper.get('[role="option"]').text()).toContain('alice@example.test')
    await wrapper.get('[role="option"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([7])
    expect(wrapper.emitted('change')!.at(-1)).toEqual([alice])
    expect(wrapper.get('input').element.value).toBe('Alice · alice@example.test')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(getById).not.toHaveBeenCalled()
  })

  it('clears an old selection when typing while keeping the new keyword and never treats numeric text as an ID', async () => {
    const wrapper = render(7)
    await flushPromises()
    await wrapper.get('input').setValue('29')
    await flushPromises()
    expect(wrapper.props('modelValue')).toBeUndefined()
    expect(wrapper.get('input').element.value).toBe('29')
    expect(wrapper.vm.hasUnselectedQuery()).toBe(true)
    expect(wrapper.emitted('change')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(300)
    expect(searchUsers).toHaveBeenLastCalledWith('29')
    expect(wrapper.emitted('update:modelValue')!.at(-1)).toEqual([undefined])
  })

  it('ignores a stale search response and sorts deleted users last', async () => {
    const older = deferred<Awaited<ReturnType<typeof searchUsers>>>()
    const newer = deferred<Awaited<ReturnType<typeof searchUsers>>>()
    vi.mocked(searchUsers).mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)
    const wrapper = render()
    await type(wrapper, 'Alice')
    await type(wrapper, 'Bob')
    newer.resolve([{ ...alice, deleted: true }, bob])
    await flushPromises()
    expect(wrapper.findAll('[role="option"]')[0]!.text()).toContain('Bob')
    older.resolve([{ id: 99, email: 'stale@example.test', deleted: false }])
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale@example.test')
    expect(wrapper.text()).toContain('admin.usage.userDeletedBadge')
  })

  it('hydrates legacy user ID links and prevents a late hydration response replacing newly typed text', async () => {
    const pending = deferred<Awaited<ReturnType<typeof getById>>>()
    vi.mocked(getById).mockReturnValueOnce(pending.promise)
    const wrapper = render(7)
    expect(getById).toHaveBeenCalledWith(7, true)
    expect(wrapper.get('input').element.value).toBe('#7')
    await wrapper.get('input').setValue('Bob')
    await flushPromises()
    pending.resolve(alice as Awaited<ReturnType<typeof getById>>)
    await flushPromises()
    expect(wrapper.get('input').element.value).toBe('Bob')
    expect(wrapper.props('modelValue')).toBeUndefined()
  })

  it('keeps labels and exact IDs in sync when an external route selects another user', async () => {
    vi.mocked(getById).mockResolvedValueOnce(alice as Awaited<ReturnType<typeof getById>>).mockResolvedValueOnce(bob as Awaited<ReturnType<typeof getById>>)
    const wrapper = render(7)
    await flushPromises()
    expect(wrapper.get('input').element.value).toBe('Alice · alice@example.test')
    await wrapper.setProps({ modelValue: 8 })
    await flushPromises()
    expect(getById).toHaveBeenLastCalledWith(8, true)
    expect(wrapper.get('input').element.value).toBe('Bob · bob@example.test')
    await wrapper.setProps({ modelValue: undefined })
    expect(wrapper.get('input').element.value).toBe('')
  })

  it('retains a legacy exact ID when its label cannot be resolved', async () => {
    vi.mocked(getById).mockRejectedValue(new Error('not found'))
    const wrapper = render(81)
    await flushPromises()
    expect(wrapper.get('input').element.value).toBe('#81')
    expect(wrapper.vm.hasUnselectedQuery()).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('supports keyboard selection, Escape, outside closing and the explicit clear action', async () => {
    vi.mocked(searchUsers).mockResolvedValue([alice, bob])
    const wrapper = render()
    await type(wrapper, 'person')
    const input = wrapper.get('input')
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(input.attributes('aria-activedescendant')).toMatch(/-0$/)
    await input.trigger('keydown', { key: 'ArrowDown' })
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.props('modelValue')).toBe(8)
    await input.trigger('focus')
    await input.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    await input.trigger('focus')
    document.body.click()
    await flushPromises()
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    await wrapper.get('button[aria-label="common.clear"]').trigger('click')
    expect(wrapper.get('input').element.value).toBe('')
    expect(wrapper.props('modelValue')).toBeUndefined()
    expect(wrapper.emitted('change')!.at(-1)).toEqual([undefined])
  })

  it('reports a committed selection during the parent synchronous change handler before the prop flush', async () => {
	vi.mocked(searchUsers).mockResolvedValue([alice])
	const wrapper = render()
	const onChange = vi.fn(() => wrapper.vm.hasUnselectedQuery())
	await wrapper.setProps({ onChange })
	await type(wrapper, 'Alice')
	expect(wrapper.vm.hasUnselectedQuery()).toBe(true)
	await wrapper.get('[role="option"]').trigger('click')
	expect(onChange).toHaveReturnedWith(false)
	await wrapper.setProps({ modelValue: undefined })
	expect(wrapper.get('input').element.value).toBe('')
  })

  it('exposes reset for an unselected query and cancels a queued debounce on unmount', async () => {
    const wrapper = render()
    await wrapper.get('input').setValue('not selected')
    expect(wrapper.vm.hasUnselectedQuery()).toBe(true)
    wrapper.vm.clear()
    await flushPromises()
    expect(wrapper.get('input').element.value).toBe('')
    expect(wrapper.vm.hasUnselectedQuery()).toBe(false)
    await wrapper.get('input').setValue('do not request')
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(300)
    expect(searchUsers).not.toHaveBeenCalled()
  })

  it('waits for IME composition to end before searching a nickname', async () => {
    const wrapper = render()
    const input = wrapper.get('input')
    await input.trigger('compositionstart')
    await input.setValue('派')
    await vi.advanceTimersByTimeAsync(400)
    expect(searchUsers).not.toHaveBeenCalled()
    await input.trigger('compositionend')
    await vi.advanceTimersByTimeAsync(300)
    expect(searchUsers).toHaveBeenLastCalledWith('派')
  })
})
