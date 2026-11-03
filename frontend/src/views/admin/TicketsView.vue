<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-4 px-4 py-6 sm:px-6">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('tickets.adminTitle') }}</h1>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ t('tickets.adminHint') }}</p>
        </div>
        <div class="flex items-center gap-2">
          <select v-model="statusFilter" class="input !w-auto !py-1.5 text-sm" @change="reload">
            <option value="">{{ t('tickets.filterAll') }}</option>
            <option value="pending">{{ t('tickets.status.pending') }}</option>
            <option value="replied">{{ t('tickets.status.replied') }}</option>
            <option value="closed">{{ t('tickets.status.closed') }}</option>
          </select>
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
              <td class="px-4 py-2.5 text-xs font-medium" :class="statusClass(ticket.status)">
                {{ t(`tickets.status.${ticket.status}`) }}
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

    <div
      v-if="detailOpen"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      @click.self="closeDetail"
    >
      <div class="flex max-h-[88vh] w-full max-w-2xl flex-col rounded-xl bg-white shadow-xl dark:bg-dark-800">
        <div class="flex items-start justify-between gap-4 border-b border-gray-100 px-5 py-3.5 dark:border-dark-700">
          <div class="min-w-0">
            <h2 class="truncate text-sm font-semibold text-gray-900 dark:text-white">
              #{{ detail?.ticket.id }} {{ detail?.ticket.title }}
            </h2>
            <p class="mt-1 flex flex-wrap items-center gap-2 text-xs">
              <span :class="statusClass(detail?.ticket.status)">{{ t(`tickets.status.${detail?.ticket.status}`) }}</span>
              <span class="text-gray-400">{{ detail?.ticket.user_email }}</span>
              <span class="text-gray-400">{{ formatTime(detail?.ticket.last_message_at) }}</span>
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
                {{ message.sender_role === 'admin' ? t('tickets.adminName') : t('tickets.userName') }}
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

        <div class="border-t border-gray-100 px-5 py-3 dark:border-dark-700">
          <textarea
            v-model="replyContent"
            class="input min-h-[80px] resize-y"
            :placeholder="t('tickets.replyPlaceholder')"
            maxlength="5000"
          />
          <div class="mt-2 flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-2">
              <button
                v-if="detail?.ticket.status !== 'closed'"
                class="btn btn-secondary !py-1.5 text-xs"
                type="button"
                :disabled="updating"
                @click="changeStatus('closed')"
              >
                {{ t('tickets.close') }}
              </button>
              <button
                v-else
                class="btn btn-secondary !py-1.5 text-xs"
                type="button"
                :disabled="updating"
                @click="changeStatus('pending')"
              >
                {{ t('tickets.reopen') }}
              </button>
            </div>
            <button class="btn btn-primary" type="button" :disabled="replying || !replyContent.trim()" @click="submitReply">
              {{ replying ? t('tickets.replying') : t('tickets.reply') }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
/**
 * 管理端工单管理
 *
 * 按状态/关键词筛，点开即可回复；回复会把工单置为「待用户回复」，
 * 也可以直接关闭或重新打开。
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import {
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

const tickets = ref<Ticket[]>([])
const loading = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const pages = ref(1)
const statusFilter = ref('')
const search = ref('')

const detailOpen = ref(false)
const detail = ref<TicketDetail | null>(null)
const replyContent = ref('')
const replying = ref(false)
const updating = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await adminListTickets({
      page: page.value,
      page_size: pageSize.value,
      status: statusFilter.value || undefined,
      search: search.value.trim() || undefined
    })
    tickets.value = res.items ?? []
    total.value = res.total ?? 0
    pages.value = res.pages ?? 1
  } catch {
    appStore.showError(t('tickets.loadFailed'))
  } finally {
    loading.value = false
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

async function openDetail(id: number) {
  detailOpen.value = true
  detail.value = null
  replyContent.value = ''
  try {
    detail.value = await adminGetTicket(id)
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
    await adminReplyTicket(id, replyContent.value.trim())
    replyContent.value = ''
    detail.value = await adminGetTicket(id)
    await load()
  } catch {
    appStore.showError(t('tickets.replyFailed'))
  } finally {
    replying.value = false
  }
}

async function changeStatus(status: TicketStatus) {
  const id = detail.value?.ticket.id
  if (!id || updating.value) return
  updating.value = true
  try {
    await adminUpdateTicket(id, status)
    detail.value = await adminGetTicket(id)
    await load()
  } catch {
    appStore.showError(t('tickets.updateFailed'))
  } finally {
    updating.value = false
  }
}

function formatTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()} ${pad(date.getHours())}:${pad(date.getMinutes())}`
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
