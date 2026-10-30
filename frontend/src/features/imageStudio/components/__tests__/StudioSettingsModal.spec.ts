import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

/**
 * 设置弹窗里的「重新获取密钥」是唯一能自救的入口。
 *
 * 背景：密钥改由系统下发后，原来的「管理连接」指向一个已删除的弹窗，
 * 用户点击毫无反应。这里锁住行为：点击真的要重新拉取密钥，
 * 并且调用的是存在的接口。
 */

const { state, ensureStudioApiKey, resetStudioApiKeyCache } = vi.hoisted(() => {
  const ensure = vi.fn(async () => 'sk-test')
  return {
    state: {
      showSettings: true,
      settings: {
        miloRouter: { connectionStatus: 'connected', billing: null },
        clearInputAfterSubmit: false,
        enterSubmit: false,
        taskCompletionNotification: false,
      },
      setShowSettings: vi.fn(),
      setSettings: vi.fn(),
      showToast: vi.fn(),
    },
    ensureStudioApiKey: ensure,
    resetStudioApiKeyCache: vi.fn(),
  }
})

vi.mock('../../stores/store', () => ({
  storeState: state,
  clearData: vi.fn(),
  exportData: vi.fn(),
  importData: vi.fn(),
}))

vi.mock('../../lib/studioAuth', () => ({
  ensureStudioApiKey,
  resetStudioApiKeyCache,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import StudioSettingsModal from '../StudioSettingsModal.vue'

function mountModal() {
  return mount(StudioSettingsModal, {
    global: { stubs: { StudioIcon: true, StudioCheckbox: true } },
  })
}

describe('StudioSettingsModal', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('提供「重新获取密钥」按钮（不再是无效的管理连接）', async () => {
    const wrapper = mountModal()
    await nextTick()

    const button = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('imageStudio.settings.refreshKey'))

    expect(button).toBeTruthy()
    expect(wrapper.text()).not.toContain('imageStudio.settings.manageConnection')
  })

  it('点击后清缓存并重新拉取密钥', async () => {
    const wrapper = mountModal()
    await nextTick()

    const button = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('imageStudio.settings.refreshKey'))!

    await button.trigger('click')
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(resetStudioApiKeyCache).toHaveBeenCalledTimes(1)
    expect(ensureStudioApiKey).toHaveBeenCalledTimes(1)
    expect(state.showToast).toHaveBeenCalledWith(
      'imageStudio.settings.refreshKeySuccess',
      'success',
    )
  })

  it('获取失败时提示错误而不是静默', async () => {
    ensureStudioApiKey.mockResolvedValueOnce('')
    const wrapper = mountModal()
    await nextTick()

    const button = wrapper
      .findAll('button')
      .find((btn) => btn.text().includes('imageStudio.settings.refreshKey'))!

    await button.trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(state.showToast).toHaveBeenCalledWith(
      'imageStudio.settings.refreshKeyFailed',
      'error',
    )
  })
})
