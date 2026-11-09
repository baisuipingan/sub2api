import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import TicketScreenshotPicker from '../TicketScreenshotPicker.vue'

const { prepare, parentPaste } = vi.hoisted(() => ({ prepare: vi.fn(), parentPaste: vi.fn() }))
vi.mock('@/lib/ticketAttachments', async importOriginal => ({ ...await importOriginal<typeof import('@/lib/ticketAttachments')>(), prepareTicketImage: prepare }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.clearAllMocks() })
function objectURLs() {
  vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:screenshot-preview'), revokeObjectURL: vi.fn() })
}

describe('ticket screenshot selection', () => {
  it('handles one pasted image once when nested in a composer with its own paste handler', async () => {
    objectURLs()
    const image = new File(['image'], 'screen.png', { type: 'image/png' })
    prepare.mockResolvedValue(image)
    const host = defineComponent({
      components: { TicketScreenshotPicker },
      setup() {
        const picker = ref<InstanceType<typeof TicketScreenshotPicker>>()
        return { picker, files: ref<File[]>([]), paste: (event: ClipboardEvent) => { parentPaste(); picker.value?.paste(event) } }
      },
      template: '<div @paste="paste"><TicketScreenshotPicker ref="picker" v-model="files" /></div>'
    })
    const wrapper = mount(host, { global: { stubs: { Icon: true, LoadingSpinner: true } } })
    const event = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(event, 'clipboardData', { value: { files: [image] } })
    wrapper.findComponent(TicketScreenshotPicker).element.dispatchEvent(event)
    await flushPromises()
    expect(prepare).toHaveBeenCalledTimes(1)
    expect(parentPaste).not.toHaveBeenCalled()
    expect(wrapper.findComponent(TicketScreenshotPicker).props('modelValue')).toEqual([image])
    wrapper.unmount()
  })

  it('disables adding but allows removing a draft screenshot after uploads are turned off', async () => {
    objectURLs()
    const file = new File(['image'], 'screen.png', { type: 'image/png' })
    const wrapper = mount(TicketScreenshotPicker, { props: { modelValue: [file], uploadsEnabled: false }, global: { stubs: { Icon: true, LoadingSpinner: true } } })
    const buttons = wrapper.findAll('button')
    expect(buttons[0]!.attributes('disabled')).toBeDefined()
    expect(buttons[1]!.attributes('disabled')).toBeUndefined()
    await buttons[1]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([[]])
    wrapper.unmount()
  })
})
