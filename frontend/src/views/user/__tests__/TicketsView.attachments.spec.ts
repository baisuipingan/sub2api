import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TicketsView from '../TicketsView.vue'
import AdminTicketsView from '../../admin/TicketsView.vue'

const mocks = vi.hoisted(() => ({
  create: vi.fn(), reply: vi.fn(), list: vi.fn(), detail: vi.fn(), status: vi.fn(), error: vi.fn(), success: vi.fn()
}))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 1 } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.error, showSuccess: mocks.success }) }))
vi.mock('@/stores/tickets', () => ({ useTicketStore: () => ({ refreshUserUnread: vi.fn(), refreshAdminUnread: vi.fn() }) }))
vi.mock('@/api/tickets', () => ({ createTicket: mocks.create, replyTicket: mocks.reply, listTickets: mocks.list, getTicket: mocks.detail, getTicketAttachmentStatus: mocks.status, deleteTicket: vi.fn(), adminGetTicket: mocks.detail, adminListTickets: mocks.list, adminReplyTicket: mocks.reply, adminDeleteTicket: vi.fn(), adminUpdateTicket: vi.fn() }))

const Dialog = defineComponent({ props: ['show'], emits: ['close'], template: '<div v-if="show"><button class="dialog-close" @click="$emit(\'close\')">close</button><slot/><slot name="footer"/></div>' })
const Field = defineComponent({ props: ['modelValue', 'id'], emits: ['update:modelValue'], template: '<input :id="id" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' })
const Picker = defineComponent({ name: 'TicketScreenshotPicker', props: ['modelValue'], emits: ['update:modelValue', 'busy'], template: '<div class="picker" />' })
function view(admin = false) {
  return mount(admin ? AdminTicketsView : TicketsView, { global: { stubs: { AppLayout: { template: '<div><slot/></div>' }, BaseDialog: Dialog, Input: Field, Textarea: Field, TicketScreenshotPicker: Picker, TicketMessageAttachments: true, LoadingSpinner: true, Icon: true, StatusBadge: true } } })
}
function button(wrapper: ReturnType<typeof view>, label: string) { return wrapper.findAll('button').find(item => item.text() === label)! }

beforeEach(() => {
  vi.clearAllMocks()
  mocks.list.mockResolvedValue({ items: [], total: 0, pages: 1 })
  mocks.status.mockResolvedValue({ enabled: true })
  mocks.create.mockResolvedValue({ id: 1 })
  mocks.reply.mockResolvedValue({ id: 3, ticket_id: 1, content: 'Reply draft', sender_role: 'user', created_at: '2026-11-09T08:00:00Z' })
})

describe('ticket attachment drafts', () => {
  it('keeps text, images and operation key on failure; clears after success', async () => {
    mocks.create.mockRejectedValueOnce({ reason: 'TICKET_STORAGE_UNAVAILABLE' })
    const wrapper = view()
    await flushPromises()
    await button(wrapper, 'tickets.create').trigger('click')
    await wrapper.find('#ticket-title').setValue('Issue')
    await wrapper.find('#ticket-content').setValue('Details')
    const image = new File(['image'], 'screen.png', { type: 'image/png' })
    wrapper.findComponent(Picker).vm.$emit('update:modelValue', [image])
    await button(wrapper, 'tickets.submit').trigger('click')
    await flushPromises()
    expect((wrapper.find('#ticket-content').element as HTMLInputElement).value).toBe('Details')
    expect(wrapper.findComponent(Picker).props('modelValue')).toEqual([image])
    const firstOptions = mocks.create.mock.calls[0]![1]
    await button(wrapper, 'tickets.submit').trigger('click')
    await flushPromises()
    expect(mocks.create.mock.calls[1]![1]).toEqual(firstOptions)
    expect(mocks.error).toHaveBeenCalledWith('tickets.attachments.storageError')
    await button(wrapper, 'tickets.create').trigger('click')
    expect((wrapper.find('#ticket-content').element as HTMLInputElement).value).toBe('')
    expect(wrapper.findComponent(Picker).props('modelValue')).toEqual([])
    wrapper.unmount()
  })

  it('treats a completed submission as successful even if refreshing the list fails', async () => {
    const wrapper = view()
    await flushPromises()
    await button(wrapper, 'tickets.create').trigger('click')
    await wrapper.find('#ticket-title').setValue('Issue')
    await wrapper.find('#ticket-content').setValue('Details')
    mocks.list.mockRejectedValueOnce(new Error('List temporarily unavailable'))
    await button(wrapper, 'tickets.submit').trigger('click')
    await flushPromises()
    expect(mocks.success).toHaveBeenCalledWith('tickets.createSuccess')
    expect(mocks.error).not.toHaveBeenCalledWith('tickets.createFailed')
    await button(wrapper, 'tickets.create').trigger('click')
    expect((wrapper.find('#ticket-content').element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
  })

  it('prevents submitting or closing while local image preparation is running', async () => {
    const wrapper = view()
    await flushPromises()
    await button(wrapper, 'tickets.create').trigger('click')
    await wrapper.find('#ticket-title').setValue('Issue')
    await wrapper.find('#ticket-content').setValue('Details')
    wrapper.findComponent(Picker).vm.$emit('busy', true)
    await flushPromises()
    expect(button(wrapper, 'tickets.submit').attributes('disabled')).toBeDefined()
    expect(button(wrapper, 'common.cancel').attributes('disabled')).toBeDefined()
    expect(mocks.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

function ticket(id: number) {
  return { id, user_id: 1, title: `Ticket ${id}`, status: 'pending', priority: 'normal', last_message_at: '2026-11-09T08:00:00Z' }
}
function ticketDetail(id: number) { return { ticket: ticket(id), messages: [{ id, content: `Message for ticket ${id}`, sender_role: 'user', created_at: '2026-11-09T08:00:00Z' }] } }
async function openListed(wrapper: ReturnType<typeof view>, id: number, admin: boolean) {
  if (admin) await wrapper.findAll('tbody tr').find(row => row.text().includes(`Ticket ${id}`))!.trigger('click')
  else await wrapper.findAll('button').find(item => item.text().includes(`Ticket ${id}`))!.trigger('click')
}

for (const admin of [false, true]) describe(`${admin ? 'admin' : 'user'} ticket detail concurrency`, () => {
  beforeEach(() => {
    mocks.list.mockResolvedValue({ items: [ticket(1), ticket(2)], total: 2, pages: 1 })
    mocks.detail.mockImplementation(async (id: number) => ticketDetail(id))
  })

  it('ignores a late response for a previously closed ticket', async () => {
    let finishFirst!: (value: ReturnType<typeof ticketDetail>) => void
    mocks.detail.mockImplementation((id: number) => id === 1 ? new Promise(resolve => { finishFirst = resolve }) : Promise.resolve(ticketDetail(id)))
    const wrapper = view(admin)
    await flushPromises()
    await openListed(wrapper, 1, admin)
    await wrapper.find('.dialog-close').trigger('click')
    await openListed(wrapper, 2, admin)
    await flushPromises()
    expect(wrapper.text()).toContain('Message for ticket 2')
    finishFirst(ticketDetail(1))
    await flushPromises()
    expect(wrapper.text()).toContain('Message for ticket 2')
    expect(wrapper.text()).not.toContain('Message for ticket 1')
    wrapper.unmount()
  })

  it('preserves a failed reply and its operation key when closed and reopened', async () => {
    mocks.reply.mockRejectedValueOnce({ reason: 'TICKET_STORAGE_UNAVAILABLE' })
    const wrapper = view(admin)
    await flushPromises()
    await openListed(wrapper, 1, admin)
    await flushPromises()
    const selector = admin ? '#admin-ticket-reply' : '#ticket-reply'
    await wrapper.find(selector).setValue('Reply draft')
    const image = new File(['image'], 'screen.png', { type: 'image/png' })
    wrapper.findComponent(Picker).vm.$emit('update:modelValue', [image])
    await button(wrapper, 'tickets.reply').trigger('click')
    await flushPromises()
    const firstOptions = mocks.reply.mock.calls[0]![2]
    await wrapper.find('.dialog-close').trigger('click')
    await openListed(wrapper, 2, admin)
    await flushPromises()
    expect((wrapper.find(selector).element as HTMLInputElement).value).toBe('')
    await wrapper.find('.dialog-close').trigger('click')
    await openListed(wrapper, 1, admin)
    await flushPromises()
    expect((wrapper.find(selector).element as HTMLInputElement).value).toBe('Reply draft')
    expect(wrapper.findComponent(Picker).props('modelValue')).toEqual([image])
    await button(wrapper, 'tickets.reply').trigger('click')
    await flushPromises()
    expect(mocks.reply.mock.calls[1]![2]).toEqual(firstOptions)
    wrapper.unmount()
  })

  it('keeps selected files removable after screenshot uploads are disabled', async () => {
    mocks.reply.mockRejectedValueOnce({ reason: 'TICKET_ATTACHMENTS_DISABLED' })
    const wrapper = view(admin)
    await flushPromises()
    await openListed(wrapper, 1, admin)
    await flushPromises()
    await wrapper.find(admin ? '#admin-ticket-reply' : '#ticket-reply').setValue('Reply draft')
    const image = new File(['image'], 'screen.png', { type: 'image/png' })
    wrapper.findComponent(Picker).vm.$emit('update:modelValue', [image])
    await button(wrapper, 'tickets.reply').trigger('click')
    await flushPromises()
    expect(wrapper.findComponent(Picker).exists()).toBe(true)
    expect(wrapper.findComponent(Picker).attributes('uploads-enabled')).toBe('false')
    expect(wrapper.findComponent(Picker).props('modelValue')).toEqual([image])
    wrapper.findComponent(Picker).vm.$emit('update:modelValue', [])
    await flushPromises()
    expect(wrapper.findComponent(Picker).exists()).toBe(false)
    await button(wrapper, 'tickets.reply').trigger('click')
    await flushPromises()
    expect(mocks.reply.mock.calls[1]![2].images).toEqual([])
    wrapper.unmount()
  })
})
