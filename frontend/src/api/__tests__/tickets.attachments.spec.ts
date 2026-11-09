import { beforeEach, describe, expect, it, vi } from 'vitest'
import { adminReplyTicket, createTicket, getTicketAttachmentStatus, getTicketAttachmentURL, replyTicket } from '../tickets'

const { post, get } = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { post, get } }))

describe('ticket attachment requests', () => {
  beforeEach(() => { vi.clearAllMocks(); post.mockResolvedValue({ data: { id: 1 } }); get.mockResolvedValue({ data: {} }) })

  it('submits text and the selected screenshots together without fixing a multipart boundary', async () => {
    const images = [new File(['first'], 'first.png', { type: 'image/png' }), new File(['second'], 'second.webp', { type: 'image/webp' })]
    await createTicket({ title: 'Issue', content: 'Details' }, { images, idempotencyKey: 'stable-operation' })
    const [path, body, config] = post.mock.calls[0]!
    expect(path).toBe('/tickets')
    expect(body).toBeInstanceOf(FormData)
    expect(body.get('title')).toBe('Issue')
    expect(body.get('content')).toBe('Details')
    expect(body.getAll('images')).toHaveLength(2)
    expect(config.headers['Idempotency-Key']).toBe('stable-operation')
    expect(config.headers['Content-Type']).toBeUndefined()
  })

  it('keeps plain text requests JSON-compatible for users and admins', async () => {
    await replyTicket(3, 'User reply', { idempotencyKey: 'reply-user' })
    await adminReplyTicket(3, 'Admin reply', { idempotencyKey: 'reply-admin' })
    expect(post).toHaveBeenNthCalledWith(1, '/tickets/3/messages', { content: 'User reply' }, { headers: { 'Idempotency-Key': 'reply-user' } })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/tickets/3/messages', { content: 'Admin reply' }, { headers: { 'Idempotency-Key': 'reply-admin' } })
  })

  it('uses the authenticated user/admin routes for status and private image authorization', async () => {
    await getTicketAttachmentStatus(true)
    await getTicketAttachmentURL(9, 'image-id', 'thumbnail', true)
    expect(get).toHaveBeenNthCalledWith(1, '/admin/tickets/attachments/status')
    expect(get).toHaveBeenNthCalledWith(2, '/admin/tickets/9/attachments/image-id/url', { params: { variant: 'thumbnail' } })
  })
})
