<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-4 px-4 py-5 sm:px-8">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('tickets.adminTitle') }}</h1>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ t('tickets.adminHint') }}</p>
        </div>
        <div class="flex items-center gap-2">
          <Select
            v-model="statusFilter"
            :options="statusOptions"
            class="w-40"
            :aria-label="t('tickets.columnStatus')"
            @change="reload"
          />
          <input
            v-model="search"
            class="input !w-56 !py-1.5 text-sm"
            :placeholder="t('tickets.searchPlaceholder')"
            @keyup.enter="reload"
          />
          <button class="btn btn-secondary !py-1.5" type="button" @click="reload">
            {{ t('tickets.search') }}
          </button>
        </div>
      </div>

      <div class="card overflow-hidden">
        <table class="w-full text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800 dark:text-dark-400">
            <tr>
              <th class="w-16 px-4 py-2.5 text-left font-medium">#</th>
              <th class="px-4 py-2.5 text-left font-medium">{{ t('tickets.columnTitle') }}</th>
              <th class="w-56 px-4 py-2.5 text-left font-medium">{{ t('tickets.columnUser') }}</th>
              <th class="w-24 px-4 py-2.5 text-left font-medium">{{ t('tickets.columnPriority') }}</th>
              <th class="w-32 px-4 py-2.5 text-left font-medium">{{ t('tickets.columnStatus') }}</th>
              <th class="w-44 px-4 py-2.5 text-left font-medium">{{ t('tickets.columnUpdated') }}</th>
              <th class="w-20 px-4 py-2.5 text-right font-medium">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-if="loading">
              <td colspan="7" class="px-4 py-10 text-center text-xs text-gray-400">{{ t('common.loading') }}</td>
            </tr>
            <tr v-else-if="tickets.length === 0">
              <td colspan="7" class="px-4 py-10 text-center text-xs text-gray-400">{{ t('tickets.emptyTitle') }}</td>
            </tr>
            <tr
              v-for="ticket in tickets"
              v-else
              :key="ticket.id"
              class="cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-800"
              @click="openDetail(ticket.id)"
            >
              <td class="px-4 py-2.5 tabular-nums text-gray-400">{{ ticket.id }}</td>
              <td class="max-w-0 truncate px-4 py-2.5 text-gray-900 dark:text-white">{{ ticket.title }}</td>
              <td class="max-w-0 truncate px-4 py-2.5 text-xs text-gray-500 dark:text-dark-300">
                {{ ticket.user_email || '-' }}
              </td>
              <td class="px-4 py-2.5">
                <span class="rounded px-1.5 py-0.5 text-[11px]" :class="priorityClass(ticket.priority)">
                  {{ t(`tickets.priority.${ticket.priority}`) }}
                </span>
              </td>
              <td class="px-4 py-2.5">
                <StatusBadge :status="ticket.status" />
              </td>
              <td class="px-4 py-2.5 text-xs tabular-nums text-gray-500">{{ formatTime(ticket.last_message_at) }}</td>
              <td class="px-4 py-2.5 text-right">
                <button class="text-xs text-primary-600 hover:underline" type="button" @click.stop="openDetail(ticket.id)">
                  {{ t('tickets.view') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="total > pageSize" class="flex items-center justify-between text-xs text-gray-500">
        <span>{{ t('tickets.totalCount', { total }) }}</span>
        <div class="flex items-center gap-2">
          <button class="btn btn-secondary !px-2.5 !py-1.5" type="button" :disabled="page <= 1" @click="changePage(page - 1)">
            {{ t('tickets.prevPage') }}
          </button>
          <span class="tabular-nums">{{ page }} / {{ pages }}</span>
          <button class="btn btn-secondary !px-2.5 !py-1.5" type="button" :disabled="page >= pages" @click="changePage(page + 1)">
            {{ t('tickets.nextPage') }}
          </button>
        </div>
      </div>
    </div>

    <BaseDialog :show="detailOpen" :title="detail ? `#${detail.ticket.id} ${detail.ticket.title}` : t('tickets.adminTitle')" width="wide" :close-on-escape="!replying && !updating && !deleting && !replyPreparing" :show-close-button="!replying && !updating && !deleting && !replyPreparing" @close="closeDetail">
      <div v-if="detail" class="space-y-4">
        <div class="flex flex-wrap items-center gap-2 text-xs text-gray-400">
          <StatusBadge :status="detail.ticket.status" />
          <span>{{ detail.ticket.user_email }}</span>
          <span>{{ formatTime(detail.ticket.last_message_at) }}</span>
        </div>
        <div v-for="message in detail.messages" :key="message.id" class="space-y-1">
          <div class="flex items-center gap-2 text-[11px] text-gray-400">
            <span class="font-medium text-gray-600 dark:text-dark-300">{{ message.sender_role === 'admin' ? t('tickets.adminName') : t('tickets.userName') }}</span>
            <span>{{ formatTime(message.created_at) }}</span>
          </div>
          <div class="rounded-lg px-3 py-2 text-sm leading-6 whitespace-pre-wrap break-words" :class="message.sender_role === 'admin' ? 'bg-primary-50 text-gray-800 dark:bg-primary-500/10 dark:text-dark-100' : 'bg-gray-50 text-gray-800 dark:bg-dark-700 dark:text-dark-100'">{{ message.content }}</div>
          <TicketMessageAttachments :ticket-id="detail.ticket.id" :owner-id="detail.ticket.user_id" :attachments="message.attachments" admin />
        </div>
      </div>
      <LoadingSpinner v-else class="mx-auto my-8 block" />
      <template #footer>
        <div v-if="detail" class="max-h-[50vh] w-full min-w-0 space-y-3 overflow-y-auto" @paste="replyPicker?.paste($event)">
          <Textarea id="admin-ticket-reply" v-model="replyContent" :maxlength="5000" :aria-label="t('tickets.replyPlaceholder')" :placeholder="t('tickets.replyPlaceholder')" :disabled="replying || updating || deleting" />
          <TicketScreenshotPicker v-if="attachmentsEnabled || replyImages.length || replyPreparing" ref="replyPicker" v-model="replyImages" :uploads-enabled="attachmentsEnabled" :disabled="replying || updating || deleting" @busy="replyPreparing = $event" />
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-2">
              <button class="btn btn-secondary !py-1.5 text-xs" type="button" :disabled="updating || replying || deleting || replyPreparing" @click="changeStatus(detail.ticket.status === 'closed' ? 'pending' : 'closed')">{{ t(detail.ticket.status === 'closed' ? 'tickets.reopen' : 'tickets.close') }}</button>
              <button class="text-xs text-gray-400 transition-colors hover:text-red-600 dark:hover:text-red-400" type="button" :disabled="deleting || replying || updating || replyPreparing" @click="removeTicket">{{ t('tickets.delete') }}</button>
            </div>
            <button class="btn btn-primary" type="button" :disabled="replying || updating || deleting || replyPreparing || !replyContent.trim()" @click="submitReply">{{ replying ? t('tickets.replying') : t('tickets.reply') }}</button>
          </div>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
/**
 * 管理端工单管理
 *
 * 按状态/关键词筛，点开即可回复；回复会把工单置为「待用户回复」，
 * 也可以直接关闭或重新打开。
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Textarea from '@/components/common/TextArea.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import TicketScreenshotPicker from '@/components/tickets/TicketScreenshotPicker.vue'
import TicketMessageAttachments from '@/components/tickets/TicketMessageAttachments.vue'
import { createTicketOperation } from '@/lib/ticketAttachments'
import { ticketErrorKey } from '@/lib/ticketErrors'
import Select from '@/components/common/Select.vue'
import StatusBadge from '@/components/tickets/TicketStatusBadge.vue'
import { useAppStore } from '@/stores/app'
import { useTicketStore } from '@/stores/tickets'
import {
  adminDeleteTicket,
  getTicketAttachmentStatus,
  adminGetTicket,
  adminListTickets,
  adminReplyTicket,
  adminUpdateTicket,
  type Ticket,
  type TicketDetail,
  type TicketStatus
} from '@/api/tickets'

const { t } = useI18n()
const appStore = useAppStore()
const ticketStore = useTicketStore()

const tickets = ref<Ticket[]>([])
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const pages = ref(1)
const statusFilter = ref('')

// 用组件库的 Select，保持与站点其它筛选器一致（原生 select 在深浅色下样式不统一）
const statusOptions = computed(() => [
  { value: '', label: t('tickets.filterAll') },
  { value: 'pending', label: t('tickets.status.pending') },
  { value: 'replied', label: t('tickets.status.replied') },
  { value: 'closed', label: t('tickets.status.closed') }
])
const search = ref('')

const attachmentsEnabled = ref(false)
const replyImages = ref<File[]>([])
const replyPreparing = ref(false)
const replyPicker = ref<InstanceType<typeof TicketScreenshotPicker>>()
let replyOperation = createTicketOperation()
const replyDrafts = new Map<number, { content: string; images: File[]; operation: ReturnType<typeof createTicketOperation> }>()
let activeTicketId: number | null = null
let detailRequest = 0
let listRequest = 0
let attachmentStatusRequest = 0

const detailOpen = ref(false)
const detail = ref<TicketDetail | null>(null)
const replyContent = ref('')
const replying = ref(false)
const updating = ref(false)
const deleting = ref(false)

async function load() {
  const request = ++listRequest
  loading.value = true
  try {
    const res = await adminListTickets({
      page: page.value,
      page_size: pageSize.value,
      status: statusFilter.value || undefined,
      search: search.value.trim() || undefined
    })
    if (request !== listRequest) return
    tickets.value = res.items ?? []
    total.value = res.total ?? 0
    pages.value = res.pages ?? 1
  } catch {
    if (request === listRequest) appStore.showError(t('tickets.loadFailed'))
  } finally {
    if (request === listRequest) loading.value = false
  }
}

function reload() {
  page.value = 1
  void load()
}

function changePage(next: number) {
  page.value = next
  void load()
}

function saveReplyDraft() {
  if (activeTicketId !== null) {
    replyDrafts.set(activeTicketId, { content: replyContent.value, images: [...replyImages.value], operation: replyOperation })
  }
}

async function refreshAttachmentStatus() {
  const request = ++attachmentStatusRequest
  try {
    const status = await getTicketAttachmentStatus(true)
    if (request === attachmentStatusRequest) attachmentsEnabled.value = status.enabled
  } catch { /* Existing screenshots and text submissions remain available. */ }
}

async function openDetail(id: number) {
  if (replying.value || deleting.value || replyPreparing.value || updating.value) return
  saveReplyDraft()
  const draft = replyDrafts.get(id)
  activeTicketId = id
  replyContent.value = draft?.content ?? ''
  replyImages.value = draft?.images ?? []
  replyOperation = draft?.operation ?? createTicketOperation()
  const request = ++detailRequest
  detailOpen.value = true
  detail.value = null
  void refreshAttachmentStatus()
  try {
    const result = await adminGetTicket(id)
    if (request !== detailRequest || !detailOpen.value || activeTicketId !== id) return
    detail.value = result
    void ticketStore.refreshAdminUnread()
  } catch {
    if (request !== detailRequest || !detailOpen.value) return
    appStore.showError(t('tickets.loadFailed'))
    closeDetail()
  }
}

function closeDetail() {
  if (replying.value || deleting.value || replyPreparing.value || updating.value) return
  saveReplyDraft()
  detailRequest++
  activeTicketId = null
  detailOpen.value = false
  detail.value = null
}

/** 删除工单（软删除）。管理端可删任意工单，用于清理垃圾/广告。 */
async function removeTicket() {
  const id = detail.value?.ticket.id
  if (!id || deleting.value || replying.value || replyPreparing.value || updating.value) return
  if (typeof window !== 'undefined' && !window.confirm(t('tickets.deleteConfirm'))) return
  deleting.value = true
  try {
    await adminDeleteTicket(id)
    replyDrafts.delete(id); activeTicketId = null; detailRequest++
    detailOpen.value = false; detail.value = null
    appStore.showSuccess(t('tickets.deleteSuccess'))
    await load()
    void ticketStore.refreshAdminUnread()
  } catch (error) {
    appStore.showError(resolveError(error, 'tickets.deleteFailed'))
  } finally {
    deleting.value = false
  }
}

async function submitReply() {
  const id = detail.value?.ticket.id
  if (!id || !replyContent.value.trim() || replying.value || updating.value || deleting.value || replyPreparing.value) return
  replying.value = true
  try {
    const content = replyContent.value.trim()
    const message = await adminReplyTicket(id, content, { images: replyImages.value, idempotencyKey: replyOperation.keyFor({ id, content }, replyImages.value) })
    replyContent.value = ''; replyImages.value = []; replyOperation.reset(); replyDrafts.delete(id)
    if (detail.value?.ticket.id === id) {
      detail.value.messages.push(message)
      detail.value.ticket.status = 'replied'
      detail.value.ticket.last_message_at = message.created_at
    }
    await load()
  } catch (error) {
    appStore.showError(resolveError(error, 'tickets.replyFailed'))
  } finally {
    replying.value = false
  }
}

async function changeStatus(status: TicketStatus) {
  const id = detail.value?.ticket.id
  if (!id || updating.value || replying.value || deleting.value || replyPreparing.value) return
  updating.value = true
  try {
    const updated = await adminUpdateTicket(id, status)
    if (detail.value?.ticket.id === id) detail.value.ticket = updated
    await load()
  } catch (error) {
    appStore.showError(resolveError(error, 'tickets.updateFailed'))
  } finally {
    updating.value = false
  }
}

/** 与用户页保持一致：能识别具体原因就给具体提示 */
function resolveError(error: unknown, fallbackKey: string): string {
  const key = ticketErrorKey(error)
  if (key === 'tickets.attachments.disabledError') {
    attachmentStatusRequest++
    attachmentsEnabled.value = false
  }
  return t(key || fallbackKey)
}

function formatTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function priorityClass(priority: string) {
  return priority === 'high'
    ? 'bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-400'
    : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'
}

onMounted(() => {
  void load()
  void refreshAttachmentStatus()
})
onUnmounted(() => { listRequest++; detailRequest++; attachmentStatusRequest++ })
</script>
