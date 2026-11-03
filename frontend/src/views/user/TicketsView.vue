<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-4 px-4 py-6 sm:px-6">
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
              class="flex w-full items-start justify-between gap-4 px-4 py-3.5 text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-800"
              @click="openDetail(ticket.id)"
            >
              <div class="min-w-0 flex-1">
                <p class="truncate text-sm font-medium text-gray-900 dark:text-white">
                  {{ ticket.title }}
                </p>
                <p class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500 dark:text-dark-400">
                  <span class="tabular-nums">#{{ ticket.id }}</span>
                  <span class="rounded px-1.5 py-0.5 text-[11px]" :class="priorityClass(ticket.priority)">
                    {{ t(`tickets.priority.${ticket.priority}`) }}
                  </span>
                  <span>{{ t('tickets.updatedAt', { time: formatTime(ticket.last_message_at) }) }}</span>
                </p>
              </div>
              <span class="shrink-0 whitespace-nowrap text-xs font-medium" :class="statusClass(ticket.status)">
                {{ t(`tickets.status.${ticket.status}`) }}
              </span>
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

    <!-- 新建工单 -->
    <div
      v-if="createOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      @click.self="closeCreate"
    >
      <div class="w-full max-w-lg rounded-xl bg-white shadow-xl dark:bg-dark-800">
        <div class="flex items-center justify-between border-b border-gray-100 px-5 py-3.5 dark:border-dark-700">
          <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('tickets.create') }}</h2>
          <button type="button" class="text-gray-400 hover:text-gray-600" @click="closeCreate">
            <Icon name="x" size="sm" />
          </button>
        </div>
        <div class="space-y-4 px-5 py-4">
          <div>
            <label class="input-label">{{ t('tickets.titleLabel') }}</label>
            <input v-model="form.title" class="input" :placeholder="t('tickets.titlePlaceholder')" maxlength="120" />
          </div>
          <div>
            <label class="input-label">{{ t('tickets.contentLabel') }}</label>
            <textarea
              v-model="form.content"
              class="input min-h-[140px] resize-y"
              :placeholder="t('tickets.contentPlaceholder')"
              maxlength="5000"
            />
          </div>
        </div>
        <div class="flex justify-end gap-2 border-t border-gray-100 px-5 py-3 dark:border-dark-700">
          <button class="btn btn-secondary" type="button" @click="closeCreate">
            {{ t('common.cancel') }}
          </button>
          <button class="btn btn-primary" type="button" :disabled="submitting || !canSubmit" @click="submitCreate">
            {{ submitting ? t('tickets.submitting') : t('tickets.submit') }}
          </button>
        </div>
      </div>
    </div>

    <!-- 工单详情 -->
    <div
      v-if="detailOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      @click.self="closeDetail"
    >
      <div class="flex max-h-[85vh] w-full max-w-2xl flex-col rounded-xl bg-white shadow-xl dark:bg-dark-800">
        <div class="flex items-start justify-between gap-4 border-b border-gray-100 px-5 py-3.5 dark:border-dark-700">
          <div class="min-w-0">
            <h2 class="truncate text-sm font-semibold text-gray-900 dark:text-white">
              {{ detail?.ticket.title }}
            </h2>
            <p class="mt-1 flex flex-wrap items-center gap-2 text-xs">
              <span :class="statusClass(detail?.ticket.status)">{{ t(`tickets.status.${detail?.ticket.status}`) }}</span>
              <span class="text-gray-400">
                {{ t('tickets.updatedAt', { time: formatTime(detail?.ticket.last_message_at) }) }}
              </span>
            </p>
          </div>
          <button type="button" class="shrink-0 text-gray-400 hover:text-gray-600" @click="closeDetail">
            <Icon name="x" size="sm" />
          </button>
        </div>

        <div class="min-h-0 flex-1 space-y-4 overflow-y-auto px-5 py-4">
          <div v-for="message in detail?.messages ?? []" :key="message.id" class="space-y-1">
            <div class="flex items-center gap-2 text-[11px] text-gray-400">
              <span class="font-medium text-gray-600 dark:text-dark-300">
                {{ message.sender_role === 'admin' ? t('tickets.adminName') : t('tickets.me') }}
              </span>
              <span>{{ formatTime(message.created_at) }}</span>
            </div>
            <div
              class="rounded-lg px-3 py-2 text-sm leading-6 whitespace-pre-wrap break-words"
              :class="
                message.sender_role === 'admin'
                  ? 'bg-primary-50 text-gray-800 dark:bg-primary-500/10 dark:text-dark-100'
                  : 'bg-gray-50 text-gray-800 dark:bg-dark-700 dark:text-dark-100'
              "
            >
              {{ message.content }}
            </div>
          </div>
        </div>

        <div v-if="detail?.ticket.status !== 'closed'" class="border-t border-gray-100 px-5 py-3 dark:border-dark-700">
          <textarea
            v-model="replyContent"
            class="input min-h-[80px] resize-y"
            :placeholder="t('tickets.replyPlaceholder')"
            maxlength="5000"
          />
          <div class="mt-2 flex justify-end">
            <button class="btn btn-primary" type="button" :disabled="replying || !replyContent.trim()" @click="submitReply">
              {{ replying ? t('tickets.replying') : t('tickets.reply') }}
            </button>
          </div>
        </div>
        <div v-else class="border-t border-gray-100 px-5 py-3 text-xs text-gray-400 dark:border-dark-700">
          {{ t('tickets.closedHint') }}
        </div>
      </div>
    </div>
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
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import {
  createTicket,
  getTicket,
  listTickets,
  replyTicket,
  type Ticket,
  type TicketDetail,
  type TicketStatus
} from '@/api/tickets'

const { t } = useI18n()
const appStore = useAppStore()

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

const detailOpen = ref(false)
const detail = ref<TicketDetail | null>(null)
const replyContent = ref('')
const replying = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await listTickets({
      page: page.value,
      page_size: pageSize.value,
      status: activeStatus.value || undefined
    })
    tickets.value = res.items ?? []
    total.value = res.total ?? 0
    pages.value = res.pages ?? 1
  } catch (error) {
    appStore.showError(t('tickets.loadFailed'))
  } finally {
    loading.value = false
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
  form.title = ''
  form.content = ''
  createOpen.value = true
}

function closeCreate() {
  createOpen.value = false
}

async function submitCreate() {
  if (!canSubmit.value || submitting.value) return
  submitting.value = true
  try {
    await createTicket({ title: form.title.trim(), content: form.content.trim() })
    createOpen.value = false
    appStore.showSuccess(t('tickets.createSuccess'))
    page.value = 1
    await load()
  } catch {
    appStore.showError(t('tickets.createFailed'))
  } finally {
    submitting.value = false
  }
}

async function openDetail(id: number) {
  detailOpen.value = true
  detail.value = null
  replyContent.value = ''
  try {
    detail.value = await getTicket(id)
  } catch {
    appStore.showError(t('tickets.loadFailed'))
    detailOpen.value = false
  }
}

function closeDetail() {
  detailOpen.value = false
  detail.value = null
}

async function submitReply() {
  const id = detail.value?.ticket.id
  if (!id || !replyContent.value.trim() || replying.value) return
  replying.value = true
  try {
    await replyTicket(id, replyContent.value.trim())
    replyContent.value = ''
    detail.value = await getTicket(id)
    await load()
  } catch {
    appStore.showError(t('tickets.replyFailed'))
  } finally {
    replying.value = false
  }
}

function formatTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

function statusClass(status?: TicketStatus) {
  if (status === 'pending') return 'text-primary-600 dark:text-primary-400'
  if (status === 'replied') return 'text-emerald-600 dark:text-emerald-400'
  return 'text-gray-400 dark:text-dark-400'
}

function priorityClass(priority: string) {
  return priority === 'high'
    ? 'bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-400'
    : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'
}

onMounted(load)
</script>
