/** The server exposes stable reasons; keep both ticket screens consistent. */
export function ticketErrorKey(error: unknown): string | undefined {
  const data = (error as { response?: { data?: { reason?: string; code?: string } }; reason?: string })
  const reason = data?.response?.data?.reason || data?.response?.data?.code || data?.reason
  const keys: Record<string, string> = {
    TICKET_CREATE_RATE_LIMITED: 'tickets.rateLimited',
    TICKET_MESSAGE_RATE_LIMITED: 'tickets.rateLimited',
    PANEL_RATE_LIMITED: 'tickets.rateLimited',
    TICKET_OPEN_LIMIT: 'tickets.openLimitReached',
    TICKET_CLOSED: 'tickets.closedHint',
    TICKET_UPLOAD_TIMEOUT: 'tickets.attachments.timeoutError',
    TICKET_ATTACHMENT_DISABLED: 'tickets.attachments.disabledError',
    TICKET_ATTACHMENTS_DISABLED: 'tickets.attachments.disabledError',
    TICKET_ATTACHMENT_TOO_LARGE: 'tickets.attachments.uploadSizeError',
    TICKET_ATTACHMENT_INVALID: 'tickets.attachments.formatError',
    TICKET_ATTACHMENT_TOO_MANY: 'tickets.attachments.countError',
    TICKET_ATTACHMENT_BUSY: 'tickets.attachments.busyError',
    TICKET_STORAGE_UNAVAILABLE: 'tickets.attachments.storageError',
    TICKET_ATTACHMENT_LIMIT: 'tickets.attachments.countError',
    TICKET_ATTACHMENT_UNAVAILABLE: 'tickets.attachments.storageError',
    IDEMPOTENCY_IN_PROGRESS: 'tickets.submissionInProgress',
    IDEMPOTENCY_PAYLOAD_MISMATCH: 'tickets.submissionChanged'
  }
  return reason ? keys[reason] : undefined
}
