/**
 * 用户工单 API
 *
 * 用户侧只能读写自己的工单；管理端接口全部走 /admin 前缀。
 */

import { apiClient } from './client'
import type { PaginatedResponse } from '@/types'

export type TicketStatus = 'pending' | 'replied' | 'closed'
export type TicketPriority = 'normal' | 'high'

export interface Ticket {
  id: number
  user_id: number
  title: string
  status: TicketStatus
  priority: TicketPriority
  last_message_at: string
  last_message_role: 'user' | 'admin'
  created_at: string
  updated_at: string
  user_email?: string
  message_count: number
  /** 对该查看方而言是否有新回复 */
  unread?: boolean
}

export interface TicketMessage {
  id: number
  ticket_id: number
  sender_id: number | null
  sender_role: 'user' | 'admin'
  content: string
  created_at: string
}

export interface TicketDetail {
  ticket: Ticket
  messages: TicketMessage[]
}

export interface TicketListParams {
  page?: number
  page_size?: number
  status?: string
  search?: string
}

/** 用户：未读工单数（侧栏红点） */
export async function getUnreadCount(): Promise<{ count: number }> {
  const { data } = await apiClient.get<{ count: number }>('/tickets/unread-count')
  return data
}

/** 管理端：未读工单数 */
export async function adminGetUnreadCount(): Promise<{ count: number }> {
  const { data } = await apiClient.get<{ count: number }>('/admin/tickets/unread-count')
  return data
}

/** 用户：我的工单列表 */
export async function listTickets(params: TicketListParams = {}): Promise<PaginatedResponse<Ticket>> {
  const { data } = await apiClient.get<PaginatedResponse<Ticket>>('/tickets', { params })
  return data
}

/** 用户：新建工单 */
export async function createTicket(payload: {
  title: string
  content: string
  priority?: TicketPriority
}): Promise<Ticket> {
  const { data } = await apiClient.post<Ticket>('/tickets', payload)
  return data
}

/** 用户：工单详情（含全部消息） */
export async function getTicket(id: number): Promise<TicketDetail> {
  const { data } = await apiClient.get<TicketDetail>(`/tickets/${id}`)
  return data
}

/** 用户：回复工单 */
export async function replyTicket(id: number, content: string): Promise<TicketMessage> {
  const { data } = await apiClient.post<TicketMessage>(`/tickets/${id}/messages`, { content })
  return data
}

/** 用户：删除自己的工单（软删除） */
export async function deleteTicket(id: number): Promise<void> {
  await apiClient.delete(`/tickets/${id}`)
}

/** 管理端：删除任意工单（软删除） */
export async function adminDeleteTicket(id: number): Promise<void> {
  await apiClient.delete(`/admin/tickets/${id}`)
}

/** 管理端：全部工单 */
export async function adminListTickets(params: TicketListParams = {}): Promise<PaginatedResponse<Ticket>> {
  const { data } = await apiClient.get<PaginatedResponse<Ticket>>('/admin/tickets', { params })
  return data
}

/** 管理端：工单详情 */
export async function adminGetTicket(id: number): Promise<TicketDetail> {
  const { data } = await apiClient.get<TicketDetail>(`/admin/tickets/${id}`)
  return data
}

/** 管理端：回复 */
export async function adminReplyTicket(id: number, content: string): Promise<TicketMessage> {
  const { data } = await apiClient.post<TicketMessage>(`/admin/tickets/${id}/messages`, { content })
  return data
}

/** 管理端：改状态 */
export async function adminUpdateTicket(id: number, status: TicketStatus): Promise<Ticket> {
  const { data } = await apiClient.put<Ticket>(`/admin/tickets/${id}`, { status })
  return data
}

const ticketsAPI = {
  deleteTicket,
  adminDeleteTicket,
  getUnreadCount,
  adminGetUnreadCount,
  listTickets,
  createTicket,
  getTicket,
  replyTicket,
  adminListTickets,
  adminGetTicket,
  adminReplyTicket,
  adminUpdateTicket
}

export default ticketsAPI
