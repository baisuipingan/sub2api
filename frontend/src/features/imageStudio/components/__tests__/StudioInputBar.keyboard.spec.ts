import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
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
import {
  getContentEditablePlainText,
  getContentEditableSelection,
  insertContentEditableText,
  setContentEditableSelection,
} from '../../lib/contentEditableMentions'
import StudioInputBar from '../StudioInputBar.vue'

enableAutoUnmount(afterEach)
afterEach(() => vi.useRealTimers())

function mountInput(attachTo?: HTMLElement) {
  return mount(StudioInputBar, {
    attachTo,
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

describe('StudioInputBar prompt synchronization', () => {
  it('renders the existing prompt when the editor first mounts', async () => {
    state.prompt = 'An existing landscape'
    const wrapper = mountInput()
    await nextTick()

    expect(wrapper.get('[contenteditable="true"]').text()).toBe(state.prompt)
    expect(state.setPrompt).not.toHaveBeenCalled()
  })

  it('restores the visible prompt when leaving and returning to the page', async () => {
    const firstVisit = mountInput()
    const editor = firstVisit.get<HTMLElement>('[contenteditable="true"]')
    editor.element.textContent = 'Keep this draft'
    await editor.trigger('input')
    firstVisit.unmount()

    const nextVisit = mountInput()
    await nextTick()
    expect(state.prompt).toBe('Keep this draft')
    expect(nextVisit.get('[contenteditable="true"]').text()).toBe('Keep this draft')
  })

  it('applies external updates after an input event leaves the prompt unchanged', async () => {
    const wrapper = mountInput()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    state.setPrompt('Original draft')
    await flushPromises()
    await editor.trigger('input')
    state.setPrompt('A restored draft')
    await flushPromises()

    expect(editor.text()).toBe('A restored draft')
  })

  it.each(['An external draft', 'Original draft'])('applies a same-tick external update to "%s"', async (externalPrompt) => {
    const wrapper = mountInput()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    state.setPrompt('Original draft')
    await flushPromises()

    editor.element.textContent = 'A typed draft'
    editor.element.dispatchEvent(new Event('input', { bubbles: true }))
    state.setPrompt(externalPrompt)
    await nextTick()

    expect(state.prompt).toBe(externalPrompt)
    expect(editor.text()).toBe(externalPrompt)
  })

  it('preserves text nodes and the caret during ordinary input', async () => {
    const wrapper = mountInput(document.body)
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    await nextTick()
    const text = document.createTextNode('A typed draft')
    editor.element.replaceChildren(text)
    editor.element.focus()
    setContentEditableSelection(editor.element, 2, 2)
    await editor.trigger('input')
    await nextTick()

    expect(state.prompt).toBe('A typed draft')
    expect(editor.element.firstChild).toBe(text)
    expect(window.getSelection()?.anchorNode).toBe(text)
    expect(window.getSelection()?.anchorOffset).toBe(2)
  })

  it('renders reference tags and a trailing newline, and responds to reference changes', async () => {
    state.inputImages = [{ id: 'reference-one', dataUrl: 'data:image/png;base64,x' }]
    state.prompt = `Use ${getSelectedImageMentionLabel(0)}\n`
    const wrapper = mountInput()
    await nextTick()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    const mention = editor.get<HTMLElement>('.mention-tag')

    expect(mention.text()).toBe('@图1')
    expect(mention.element.contentEditable).toBe('false')
    expect(getContentEditablePlainText(editor.element)).toBe(state.prompt)
    expect(editor.find('br[data-sentinel-br="true"]').exists()).toBe(true)

    await editor.trigger('input')
    state.inputImages = []
    await nextTick()
    expect(editor.find('.mention-tag').exists()).toBe(false)
    expect(editor.element.textContent).toBe('Use @图1\n')
    expect(editor.find('br[data-sentinel-br="true"]').exists()).toBe(true)
  })

  it('clears the visible editor when the prompt is cleared externally', async () => {
    const wrapper = mountInput()
    state.setPrompt('A draft')
    await flushPromises()
    state.setPrompt('')
    await flushPromises()

    expect(wrapper.get('[contenteditable="true"]').element.textContent).toBe('')
    expect(wrapper.text()).toContain('imageStudio.input.placeholder')
  })
})

describe('StudioInputBar keyboard', () => {
  it('restores the caret after Enter before timers run without refocusing the editor', async () => {
    state.prompt = 'A landscape'
    const wrapper = mountInput(document.body)
    await nextTick()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    editor.element.focus()
    setContentEditableSelection(editor.element, 2, 2)
    const focus = vi.spyOn(editor.element, 'focus')

    await editor.trigger('keydown', { key: 'Enter' })

    expect(state.prompt).toBe('A \nlandscape')
    expect(getContentEditableSelection(editor.element)).toEqual({ start: 3, end: 3 })
    expect(document.activeElement).toBe(editor.element)
    expect(focus).not.toHaveBeenCalled()
  })

  it('keeps consecutive Enter presses and immediate typing at the current caret', async () => {
    state.prompt = 'A landscape'
    const wrapper = mountInput(document.body)
    await nextTick()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    editor.element.focus()
    setContentEditableSelection(editor.element, state.prompt.length, state.prompt.length)

    await editor.trigger('keydown', { key: 'Enter' })
    await editor.trigger('keydown', { key: 'Enter' })
    expect(insertContentEditableText(editor.element, 'More detail')).toBe(true)
    await editor.trigger('input')

    expect(state.prompt).toBe('A landscape\n\nMore detail')
    expect(getContentEditablePlainText(editor.element)).toBe(state.prompt)
    expect(getContentEditableSelection(editor.element)).toEqual({
      start: state.prompt.length,
      end: state.prompt.length,
    })
  })

  it('restores the caret after a multiline paste before immediate typing', async () => {
    state.prompt = 'A landscape'
    const wrapper = mountInput(document.body)
    await nextTick()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    editor.element.focus()
    setContentEditableSelection(editor.element, 2, state.prompt.length)
    await editor.trigger('paste', {
      clipboardData: { files: [], getData: () => 'forest\r\nwith mist' },
    })
    expect(insertContentEditableText(editor.element, ' and light')).toBe(true)
    await editor.trigger('input')

    expect(state.prompt).toBe('A forest\nwith mist and light')
    expect(getContentEditablePlainText(editor.element)).toBe(state.prompt)
  })

  it('leaves a moved focus and selection alone while a newline update is pending', async () => {
    state.prompt = 'A landscape'
    const wrapper = mountInput(document.body)
    await nextTick()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    editor.element.focus()
    setContentEditableSelection(editor.element, state.prompt.length, state.prompt.length)
    const update = editor.trigger('keydown', { key: 'Enter' })
    const generate = wrapper.findAll('button').find((button) => button.text() === 'imageStudio.input.generateCount')!
    generate.element.focus()
    window.getSelection()?.removeAllRanges()
    await update

    expect(document.activeElement).toBe(generate.element)
    expect(window.getSelection()?.rangeCount).toBe(0)
  })

  it('does not restore an obsolete caret after an external draft replaces a pending newline', async () => {
    state.prompt = 'A landscape'
    const wrapper = mountInput(document.body)
    await nextTick()
    const editor = wrapper.get<HTMLElement>('[contenteditable="true"]')
    editor.element.focus()
    setContentEditableSelection(editor.element, state.prompt.length, state.prompt.length)
    const update = editor.trigger('keydown', { key: 'Enter' })
    state.setPrompt('Replacement draft')
    await update

    expect(getContentEditablePlainText(editor.element)).toBe('Replacement draft')
    expect(getContentEditableSelection(editor.element)).toEqual({ start: 0, end: 0 })
  })

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
    expect(editor.element.textContent).toBe('A landscape\n')
    expect(editor.find('br[data-sentinel-br="true"]').exists()).toBe(true)
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
