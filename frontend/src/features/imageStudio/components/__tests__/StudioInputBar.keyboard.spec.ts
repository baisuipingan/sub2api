import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { InputImage } from '../../types'

const { rawState, submitTask } = vi.hoisted(() => ({
  rawState: {
    prompt: '',
    inputImages: [] as InputImage[],
    maskDraft: null,
    tasks: [],
    modelCatalog: [],
    imagePricing: {},
    catalogStatus: 'ready',
    catalogError: undefined,
    settings: {
      selectedModelId: 'gpt-image-2',
      enterSubmit: true,
      miloRouter: { connectionStatus: 'connected' },
    },
    params: { size: 'auto', resolution: 'auto', n: 1 },
    setPrompt: vi.fn(),
    setParams: vi.fn(),
    showToast: vi.fn(),
    refreshImageModelCatalog: vi.fn(),
  },
  submitTask: vi.fn(),
}))

vi.mock('../../stores/store', async () => ({
  storeState: (await import('vue')).reactive(rawState),
  submitTask,
  addImageFromFile: vi.fn(),
  deleteImageIfUnreferenced: vi.fn(),
  stopTaskGeneration: vi.fn(),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import { storeState as state } from '../../stores/store'
import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'
import { getSelectedImageMentionLabel } from '../../lib/promptImageMentions'
import { setContentEditableSelection } from '../../lib/contentEditableMentions'
import StudioInputBar from '../StudioInputBar.vue'

enableAutoUnmount(afterEach)
afterEach(() => vi.useRealTimers())

function mountInput() {
  return mount(StudioInputBar, {
    global: { stubs: { StudioIcon: true, StudioDragUploadOverlay: true, StudioInputParamsPanel: true } },
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  window.getSelection()?.removeAllRanges()
  state.prompt = ''
  state.inputImages = []
  state.modelCatalog = createImageModelCatalogFromModelList({ data: [{ id: 'gpt-image-2' }] })
  state.settings.enterSubmit = true
  state.settings.miloRouter.connectionStatus = 'connected'
  state.catalogStatus = 'ready'
  state.setPrompt = vi.fn((prompt: string) => { state.prompt = prompt })
})

describe('StudioInputBar keyboard', () => {
  it.each([
    { preference: true, shiftKey: false },
    { preference: false, shiftKey: false },
    { preference: true, shiftKey: true },
  ])('Enter inserts a newline with legacy preference=$preference and Shift=$shiftKey', async ({ preference, shiftKey }) => {
    state.settings.enterSubmit = preference
    const wrapper = mountInput()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    editor.element.textContent = 'A landscape'
    await editor.trigger('input')
    setContentEditableSelection(editor.element, 'A landscape'.length, 'A landscape'.length)
    await editor.trigger('keydown', { key: 'Enter', shiftKey })

    expect(state.prompt).toBe('A landscape\n')
    expect(submitTask).not.toHaveBeenCalled()
  })

  it('does not intercept Enter during input method composition', async () => {
    const wrapper = mountInput()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    const event = new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true, cancelable: true })
    editor.element.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(false)
    expect(state.setPrompt).not.toHaveBeenCalled()
    expect(submitTask).not.toHaveBeenCalled()
  })

  it('still uses Enter to confirm a reference mention without generating', async () => {
    state.inputImages = [{ id: 'reference-one', dataUrl: 'data:image/png;base64,x' }]
    const wrapper = mountInput()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    editor.element.textContent = '@'
    await editor.trigger('input')
    setContentEditableSelection(editor.element, 1, 1)
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true)
    await editor.trigger('keydown', { key: 'Enter' })

    expect(state.prompt).toBe(getSelectedImageMentionLabel(0))
    expect(submitTask).not.toHaveBeenCalled()
  })

  it('keeps generation on the existing generate button', async () => {
    const wrapper = mountInput()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    editor.element.textContent = 'A landscape'
    await editor.trigger('input')
    const generate = wrapper.findAll('button').find((button) => button.text() === 'imageStudio.input.generateCount')!
    expect(generate.attributes('disabled')).toBeUndefined()
    await generate.trigger('click')
    await flushPromises()

    expect(submitTask).toHaveBeenCalledTimes(1)
  })
})
