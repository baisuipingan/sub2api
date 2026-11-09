import { describe, expect, it, vi } from 'vitest'
import { createTicketOperation, inspectTicketImage, prepareTicketImage, TicketImageError, TICKET_SOURCE_MAX_BYTES } from '../ticketAttachments'
import { ticketErrorKey } from '../ticketErrors'

function png(width: number, height: number, animated = false) {
  const bytes = new Uint8Array(animated ? 45 : 33)
  bytes.set([137, 80, 78, 71, 13, 10, 26, 10])
  const view = new DataView(bytes.buffer)
  view.setUint32(8, 13)
  bytes.set([73, 72, 68, 82], 12)
  view.setUint32(16, width); view.setUint32(20, height)
  if (animated) bytes.set([97, 99, 84, 76], 37)
  return bytes
}

describe('ticket screenshot preparation', () => {
  it('reads image dimensions without decoding and rejects excessive pixel counts', () => {
    expect(inspectTicketImage(png(1920, 1080))).toEqual({ width: 1920, height: 1080, mime: 'image/png' })
    expect(() => inspectTicketImage(png(6000, 4000))).toThrowError(new TicketImageError('pixels'))
    expect(() => inspectTicketImage(png(1920, 1080, true))).toThrowError(new TicketImageError('format'))
  })

  it('rejects disguised formats and animated WebP', () => {
    expect(() => inspectTicketImage(new TextEncoder().encode('<svg width="10" height="10"/>'))).toThrowError(new TicketImageError('format'))
    const webp = new Uint8Array(30)
    webp.set(new TextEncoder().encode('RIFF'), 0)
    webp.set(new TextEncoder().encode('WEBPVP8X'), 8)
    new DataView(webp.buffer).setUint32(16, 10, true)
    webp[20] = 2
    expect(() => inspectTicketImage(webp)).toThrowError(new TicketImageError('format'))
  })

  it('caps source bytes before browser decoding', async () => {
    const decoder = vi.fn()
    vi.stubGlobal('Image', decoder)
    await expect(prepareTicketImage(new File([new Uint8Array(TICKET_SOURCE_MAX_BYTES + 1)], 'large.png', { type: 'image/png' })))
      .rejects.toThrowError(new TicketImageError('size'))
    expect(decoder).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('keeps retry keys stable and changes them for edits or different files with matching metadata', () => {
    const operation = createTicketOperation()
    const first = new File(['one'], 'screen.png', { type: 'image/png', lastModified: 1 })
    const changed = new File(['two'], 'screen.png', { type: 'image/png', lastModified: 1 })
    const fields = { title: 'Problem', content: 'Please help' }
    const key = operation.keyFor(fields, [first])
    expect(operation.keyFor({ ...fields }, [first])).toBe(key)
    const replaced = operation.keyFor(fields, [changed])
    expect(replaced).not.toBe(key)
    expect(operation.keyFor({ ...fields, content: 'More detail' }, [changed])).not.toBe(replaced)
  })

  it('maps attachment and open-ticket errors consistently in both screens', () => {
    expect(ticketErrorKey({ response: { data: { reason: 'TICKET_OPEN_LIMIT' } } })).toBe('tickets.openLimitReached')
    expect(ticketErrorKey({ reason: 'TICKET_ATTACHMENT_TOO_MANY' })).toBe('tickets.attachments.countError')
    expect(ticketErrorKey({ reason: 'TICKET_STORAGE_UNAVAILABLE' })).toBe('tickets.attachments.storageError')
  })
})
