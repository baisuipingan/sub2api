<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-4 px-4 py-5 sm:px-8">
      <!-- 标题 + 新建 -->
      <div class="flex items-center justify-between gap-3">
        <h1 class="text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('tickets.myTitle') }}
        </h1>
        <button class="btn btn-primary" type="button" @click="openCreate">
          <Icon name="plus" size="sm" />
          {{ t('tickets.create') }}
        </button>
      </div>

      <!-- 筛选 -->
      <div class="flex flex-wrap items-center gap-2">
        <button
          v-for="tab in statusTabs"
          :key="tab.value"
          type="button"
          class="rounded-full px-3 py-1.5 text-xs font-medium transition-colors"
          :class="
            activeStatus === tab.value
              ? 'bg-primary-600 text-white'
              : 'bg-white text-gray-600 ring-1 ring-gray-200 hover:bg-gray-50 dark:bg-dark-800 dark:text-dark-300 dark:ring-dark-600'
          "
          @click="selectStatus(tab.value)"
        >
          {{ t(tab.labelKey) }}
        </button>
      </div>

      <!-- 列表 -->
      <div class="card overflow-hidden">
        <div v-if="loading" class="divide-y divide-gray-100 dark:divide-dark-700">
          <div v-for="i in 3" :key="i" class="animate-pulse px-4 py-4">
            <div class="h-4 w-1/3 rounded bg-gray-200 dark:bg-dark-700" />
            <div class="mt-2 h-3 w-1/4 rounded bg-gray-100 dark:bg-dark-700" />
          </div>
        </div>

        <div
          v-else-if="tickets.length === 0"
          class="flex flex-col items-center justify-center gap-2 px-6 py-16 text-center"
        >
          <Icon name="chat" size="lg" class="text-gray-300 dark:text-dark-600" />
          <p class="text-sm font-medium text-gray-700 dark:text-dark-200">
            {{ t('tickets.emptyTitle') }}
          </p>
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('tickets.emptyHint') }}</p>
        </div>

        <ul v-else class="divide-y divide-gray-100 dark:divide-dark-700">
          <li v-for="ticket in tickets" :key="ticket.id">
            <button
              type="button"
              class="flex w-full items-center justify-between gap-4 px-4 py-4 text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-800"
              @click="openDetail(ticket.id)"
            >
              <div class="min-w-0 flex-1">
                <p class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-white">
                  <span
                    v-if="ticket.unread"
                    class="h-1.5 w-1.5 shrink-0 rounded-full bg-primary-500"
                    :aria-label="t('tickets.unreadHint')"
                  />
                  <span class="truncate">{{ ticket.title }}</span>
                </p>
                <p class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500 dark:text-dark-400">
                  <span class="tabular-nums">#{{ ticket.id }}</span>
                  <span class="rounded px-1.5 py-0.5 text-[11px]" :class="priorityClass(ticket.priority)">
                    {{ t(`tickets.priority.${ticket.priority}`) }}
                  </span>
                  <span>{{ t('tickets.updatedAt', { time: formatTime(ticket.last_message_at) }) }}</span>
                </p>
              </div>
              <StatusBadge :status="ticket.status" />
            </button>
          </li>
        </ul>
      </div>

      <!-- 分页 -->
      <div v-if="total > pageSize" class="flex items-center justify-between text-xs text-gray-500">
        <span>{{ t('tickets.totalCount', { total }) }}</span>
        <div class="flex items-center gap-2">
          <button
            class="btn btn-secondary !px-2.5 !py-1.5"
            type="button"
            :disabled="page <= 1"
            @click="changePage(page - 1)"
          >
            {{ t('tickets.prevPage') }}
          </button>
          <span class="tabular-nums">{{ page }} / {{ pages }}</span>
          <button
            class="btn btn-secondary !px-2.5 !py-1.5"
            type="button"
            :disabled="page >= pages"
            @click="changePage(page + 1)"
          >
            {{ t('tickets.nextPage') }}
          </button>
        </div>
      </div>
    </div>

    <BaseDialog :show="createOpen" :title="t('tickets.create')" :close-on-escape="!submitting && !createPreparing" :show-close-button="!submitting && !createPreparing" @close="closeCreate">
      <div class="space-y-4" @paste="createPicker?.paste($event)">
        <Input id="ticket-title" v-model="form.title" :maxlength="120" :label="t('tickets.titleLabel')" :placeholder="t('tickets.titlePlaceholder')" :disabled="submitting" />
        <Textarea id="ticket-content" v-model="form.content" :maxlength="5000" :label="t('tickets.contentLabel')" :placeholder="t('tickets.contentPlaceholder')" :rows="5" :disabled="submitting" />
        <TicketScreenshotPicker v-if="attachmentsEnabled || createImages.length || createPreparing" ref="createPicker" v-model="createImages" :uploads-enabled="attachmentsEnabled" :disabled="submitting" @busy="createPreparing = $event" />
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('tickets.openLimitHint') }}</p>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" type="button" :disabled="submitting || createPreparing" @click="closeCreate">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" type="button" :disabled="submitting || createPreparing || !canSubmit" @click="submitCreate">
            {{ submitting ? t('tickets.submitting') : t('tickets.submit') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog :show="detailOpen" :title="detail?.ticket.title || t('tickets.myTitle')" width="wide" :close-on-escape="!replying && !deleting && !replyPreparing" :show-close-button="!replying && !deleting && !replyPreparing" @close="closeDetail">
      <div v-if="detail" class="space-y-4">
        <div class="flex flex-wrap items-center gap-2 text-xs text-gray-400">
          <StatusBadge :status="detail.ticket.status" />
          <span>#{{ detail.ticket.id }}</span>
          <span>{{ t('tickets.updatedAt', { time: formatTime(detail.ticket.last_message_at) }) }}</span>
        </div>
        <div v-for="message in detail.messages" :key="message.id" class="space-y-1">
          <div class="flex items-center gap-2 text-[11px] text-gray-400">
            <span class="font-medium text-gray-600 dark:text-dark-300">{{ message.sender_role === 'admin' ? t('tickets.adminName') : t('tickets.me') }}</span>
            <span>{{ formatTime(message.created_at) }}</span>
          </div>
          <div class="rounded-lg px-3 py-2 text-sm leading-6 whitespace-pre-wrap break-words" :class="message.sender_role === 'admin' ? 'bg-primary-50 text-gray-800 dark:bg-primary-500/10 dark:text-dark-100' : 'bg-gray-50 text-gray-800 dark:bg-dark-700 dark:text-dark-100'">{{ message.content }}</div>
          <TicketMessageAttachments :ticket-id="detail.ticket.id" :owner-id="detail.ticket.user_id" :attachments="message.attachments" />
        </div>
      </div>
      <LoadingSpinner v-else class="mx-auto my-8 block" />
      <template #footer>
        <div v-if="detail" class="max-h-[50vh] w-full min-w-0 space-y-3 overflow-y-auto" @paste="replyPicker?.paste($event)">
          <template v-if="detail.ticket.status !== 'closed'">
            <Textarea id="ticket-reply" v-model="replyContent" :maxlength="5000" :aria-label="t('tickets.replyPlaceholder')" :placeholder="t('tickets.replyPlaceholder')" :disabled="replying" />
            <TicketScreenshotPicker v-if="attachmentsEnabled || replyImages.length || replyPreparing" ref="replyPicker" v-model="replyImages" :uploads-enabled="attachmentsEnabled" :disabled="replying" @busy="replyPreparing = $event" />
          </template>
          <p v-else class="text-xs text-gray-400">{{ t('tickets.closedHint') }}</p>
          <div class="flex items-center justify-between gap-3">
            <button class="text-xs text-gray-400 transition-colors hover:text-red-600 dark:hover:text-red-400" type="button" :disabled="deleting || replying || replyPreparing" @click="removeTicket">{{ t('tickets.delete') }}</button>
            <button v-if="detail.ticket.status !== 'closed'" class="btn btn-primary" type="button" :disabled="replying || replyPreparing || !replyContent.trim()" @click="submitReply">{{ replying ? t('tickets.replying') : t('tickets.reply') }}</button>
          </div>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
/**
 * 用户工单页
 *
 * 交互与常见工单系统一致：列表 → 新建 / 打开详情 → 追问。
 * 状态由后端根据「最后一条消息是谁发的」自动流转，前端只做展示，
 * 避免两边各算一套状态导致不一致。
 */
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Input from '@/components/common/Input.vue'
import Textarea from '@/components/common/TextArea.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import TicketScreenshotPicker from '@/components/tickets/TicketScreenshotPicker.vue'
import TicketMessageAttachments from '@/components/tickets/TicketMessageAttachments.vue'
import { createTicketOperation } from '@/lib/ticketAttachments'
import { ticketErrorKey } from '@/lib/ticketErrors'
import StatusBadge from '@/components/tickets/TicketStatusBadge.vue'
import { useAppStore } from '@/stores/app'
import { useTicketStore } from '@/stores/tickets'
import {
  createTicket,
  getTicketAttachmentStatus,
  deleteTicket,
  getTicket,
  listTickets,
  replyTicket,
  type Ticket,
  type TicketDetail
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
const activeStatus = ref<string>('')

const statusTabs = [
  { value: '', labelKey: 'tickets.filterAll' },
  { value: 'pending', labelKey: 'tickets.status.pending' },
  { value: 'replied', labelKey: 'tickets.status.replied' },
  { value: 'closed', labelKey: 'tickets.status.closed' }
] as const

const createOpen = ref(false)
const form = reactive({ title: '', content: '' })
const submitting = ref(false)
const canSubmit = computed(() => form.title.trim() !== '' && form.content.trim() !== '')

const attachmentsEnabled = ref(false)
const createImages = ref<File[]>([])
const replyImages = ref<File[]>([])
const createPreparing = ref(false)
const replyPreparing = ref(false)
const createPicker = ref<InstanceType<typeof TicketScreenshotPicker>>()
const replyPicker = ref<InstanceType<typeof TicketScreenshotPicker>>()
const createOperation = createTicketOperation()
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
const deleting = ref(false)

async function load() {
  const request = ++listRequest
  loading.value = true
  try {
    const res = await listTickets({
      page: page.value,
      page_size: pageSize.value,
      status: activeStatus.value || undefined
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

function selectStatus(status: string) {
  activeStatus.value = status
  page.value = 1
  void load()
}

function changePage(next: number) {
  page.value = next
  void load()
}

function openCreate() {
  void refreshAttachmentStatus()
  createOpen.value = true
}

function closeCreate() {
  if (submitting.value || createPreparing.value) return
  createOpen.value = false
}

async function submitCreate() {
  if (!canSubmit.value || submitting.value || createPreparing.value) return
  submitting.value = true
  try {
    const payload = { title: form.title.trim(), content: form.content.trim() }
    await createTicket(payload, { images: createImages.value, idempotencyKey: createOperation.keyFor(payload, createImages.value) })
    form.title = ''; form.content = ''; createImages.value = []; createOperation.reset()
    createOpen.value = false
    appStore.showSuccess(t('tickets.createSuccess'))
    page.value = 1
    await load()
  } catch (error) {
    appStore.showError(resolveError(error, 'tickets.createFailed'))
  } finally {
    submitting.value = false
  }
}

function saveReplyDraft() {
  if (activeTicketId !== null) {
    replyDrafts.set(activeTicketId, { content: replyContent.value, images: [...replyImages.value], operation: replyOperation })
  }
}

async function refreshAttachmentStatus() {
  const request = ++attachmentStatusRequest
  try {
    const status = await getTicketAttachmentStatus()
    if (request === attachmentStatusRequest) attachmentsEnabled.value = status.enabled
  } catch { /* Existing screenshots and text submissions remain available. */ }
}

async function openDetail(id: number) {
  if (replying.value || deleting.value || replyPreparing.value) return
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
    const result = await getTicket(id)
    if (request !== detailRequest || !detailOpen.value || activeTicketId !== id) return
    detail.value = result
    void ticketStore.refreshUserUnread()
  } catch {
    if (request !== detailRequest || !detailOpen.value) return
    appStore.showError(t('tickets.loadFailed'))
    closeDetail()
  }
}

function closeDetail() {
  if (replying.value || deleting.value || replyPreparing.value) return
  saveReplyDraft()
  detailRequest++
  activeTicketId = null
  detailOpen.value = false
  detail.value = null
}

/** 删除当前工单。软删除，管理端仍可追溯。 */
async function removeTicket() {
  const id = detail.value?.ticket.id
  if (!id || deleting.value || replying.value || replyPreparing.value) return
  if (typeof window !== 'undefined' && !window.confirm(t('tickets.deleteConfirm'))) return
  deleting.value = true
  try {
    await deleteTicket(id)
    replyDrafts.delete(id); activeTicketId = null; detailRequest++
    detailOpen.value = false; detail.value = null
    appStore.showSuccess(t('tickets.deleteSuccess'))
    await load()
    void ticketStore.refreshUserUnread()
  } catch (error) {
    appStore.showError(resolveError(error, 'tickets.deleteFailed'))
  } finally {
    deleting.value = false
  }
}

async function submitReply() {
  const id = detail.value?.ticket.id
  if (!id || !replyContent.value.trim() || replying.value || replyPreparing.value) return
  replying.value = true
  try {
    const content = replyContent.value.trim()
    const message = await replyTicket(id, content, { images: replyImages.value, idempotencyKey: replyOperation.keyFor({ id, content }, replyImages.value) })
    replyContent.value = ''; replyImages.value = []; replyOperation.reset(); replyDrafts.delete(id)
    if (detail.value?.ticket.id === id) {
      detail.value.messages.push(message)
      detail.value.ticket.status = 'pending'
      detail.value.ticket.last_message_at = message.created_at
    }
    await load()
  } catch (error) {
    appStore.showError(resolveError(error, 'tickets.replyFailed'))
  } finally {
    replying.value = false
  }
}

/**
 * 把后端错误翻成用户能看懂的一句话。
 *
 * 后端对已关闭工单、限流等都有明确 reason，直接吞成「提交失败」会让用户反复重试；
 * 这里优先按 reason 给出具体原因，认不出来才退回通用文案。
 */
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
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
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
