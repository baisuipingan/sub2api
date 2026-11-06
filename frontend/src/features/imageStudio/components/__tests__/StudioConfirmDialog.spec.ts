import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

const { action, setConfirmDialog, initialState } = vi.hoisted(() => ({
  action: vi.fn<() => void | Promise<void>>(),
  setConfirmDialog: vi.fn(),
  initialState: { confirmDialog: null as null | { title: string; message: string; tone?: string; confirmText?: string; cancelText?: string; action: () => void | Promise<void> } },
}))
vi.mock('../../stores/store', async () => ({ storeState: (await import('vue')).reactive({ ...initialState, setConfirmDialog }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

import { storeState as state } from '../../stores/store'
import StudioConfirmDialog from '../StudioConfirmDialog.vue'

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.clearAllMocks()
  state.confirmDialog = { title: 'Remove image?', message: 'History will be cleared.', tone: 'danger', confirmText: 'Remove', cancelText: 'Keep', action }
})

function mountDialog() {
  return mount(StudioConfirmDialog, { global: { stubs: { Teleport: true, Transition: false } } })
}

describe('StudioConfirmDialog shared confirmation', () => {
  it('uses common ConfirmDialog with translated/custom labels and danger styling', async () => {
    const wrapper = mountDialog()
    const common = wrapper.getComponent(ConfirmDialog)
    expect(common.props()).toMatchObject({ show: true, title: 'Remove image?', message: 'History will be cleared.', danger: true, confirmText: 'Remove', cancelText: 'Keep' })
    expect(wrapper.getComponent(BaseDialog).props()).toMatchObject({ zIndex: 110, closeOnEscape: false })
    await wrapper.findAll('button').find((button) => button.text() === 'Keep')!.trigger('click')
    expect(setConfirmDialog).toHaveBeenCalledWith(null)
    expect(action).not.toHaveBeenCalled()
  })

  it('keeps the dialog until an asynchronous action settles, then closes', async () => {
    let finish!: () => void
    action.mockReturnValueOnce(new Promise<void>((resolve) => { finish = resolve }))
    const wrapper = mountDialog()
    await wrapper.findAll('button').find((button) => button.text() === 'Remove')!.trigger('click')
    expect(action).toHaveBeenCalledTimes(1)
    expect(setConfirmDialog).not.toHaveBeenCalled()
    finish()
    await flushPromises()
    expect(setConfirmDialog).toHaveBeenCalledWith(null)
  })

  it('does not close a newer confirmation when an older action settles', async () => {
    let finish!: () => void
    action.mockReturnValueOnce(new Promise<void>((resolve) => { finish = resolve }))
    const wrapper = mountDialog()
    await wrapper.findAll('button').find((button) => button.text() === 'Remove')!.trigger('click')
    state.confirmDialog = { title: 'New request', message: 'Keep this open', action: vi.fn() }
    finish()
    await flushPromises()
    expect(setConfirmDialog).not.toHaveBeenCalled()
  })

  it('keeps Escape under the studio modal stack and cancels without running the action', () => {
    mountDialog()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(setConfirmDialog).toHaveBeenCalledTimes(1)
    expect(action).not.toHaveBeenCalled()
  })
})
